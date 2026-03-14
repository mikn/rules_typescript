/**
 * tsserver-hook-worker.js — Background worker for the Bazel-aware tsserver hook.
 *
 * Runs in a worker thread (spawned by tsserver-hook.js).
 * Builds a resolution map from:
 *   1. npm packages in the Bazel @npm external repo (from `bazel info output_base`).
 *   2. Internal ts_compile packages (from `bazel query`).
 *   3. Path-alias directives (# gazelle:ts_path_alias) in BUILD files.
 *
 * Sends the map to the main thread via postMessage, then sets up file-system
 * watches to rebuild the map when BUILD files or pnpm-lock.yaml change.
 *
 * Design constraints:
 *   - Zero npm dependencies (Node.js builtins only).
 *   - Must not block — all Bazel invocations use execSync with a timeout.
 *   - Must degrade gracefully if Bazel is unavailable.
 */

'use strict';

const { parentPort, workerData } = require('worker_threads');
const { execSync } = require('child_process');
const path = require('path');
const fs = require('fs');

const { workspaceRoot, outputBase: providedOutputBase } = workerData;

const DEBUG = !!process.env.TSSERVER_HOOK_DEBUG;

function log(msg) {
  if (DEBUG) {
    process.stderr.write(`[tsserver-hook-worker] ${msg}\n`);
  }
}

// ── Resolution map builder ────────────────────────────────────────────────────

/**
 * Build the full resolution map and return it as a plain object.
 * Each key is a module name; each value is an absolute path to a .d.ts / .ts.
 * Keys prefixed with "__alias__" represent path-alias prefix mappings.
 *
 * @returns {Record<string, string>}
 */
function buildResolutionMap() {
  const map = {};

  // Step 1: npm packages from the Bazel external @npm repo
  // Use the pre-computed output base (from workerData.outputBase) when
  // available — this is critical when the worker runs inside a `bazel test`
  // invocation that holds the Bazel server lock, preventing `bazel info` from
  // running concurrently.
  let resolvedOutputBase = (providedOutputBase || '').trim();

  if (!resolvedOutputBase) {
    try {
      resolvedOutputBase = execSync('bazel info output_base', {
        cwd: workspaceRoot,
        encoding: 'utf8',
        timeout: 15000,
        stdio: ['ignore', 'pipe', 'ignore'],
      }).trim();
    } catch (e) {
      log(`bazel info output_base failed: ${e.message} — skipping npm resolution`);
    }
  }

  if (resolvedOutputBase) {
    const npmDir = findNpmExternalDir(resolvedOutputBase);
    if (npmDir) {
      log(`scanning npm packages in ${npmDir}`);
      scanNpmPackages(npmDir, map);
    } else {
      log('no @npm external dir found — skipping npm resolution');
    }
  }

  // Step 2: internal ts_compile packages
  try {
    const queryResult = execSync(
      "bazel query 'kind(\"ts_compile rule\", //...)' --output=package 2>/dev/null",
      {
        cwd: workspaceRoot,
        encoding: 'utf8',
        timeout: 45000,
        stdio: ['ignore', 'pipe', 'ignore'],
        shell: true,
      }
    ).trim();

    if (queryResult) {
      const packages = queryResult.split('\n').filter(Boolean);
      log(`found ${packages.length} ts_compile packages`);
      for (const pkg of packages) {
        const srcDir = path.join(workspaceRoot, pkg);
        const binDir = path.join(workspaceRoot, 'bazel-bin', pkg);
        scanPackageForResolution(pkg, srcDir, binDir, map);
      }
    }
  } catch (e) {
    log(`bazel query failed: ${e.message} — skipping internal package resolution`);
  }

  // Step 3: path aliases from BUILD files
  try {
    scanPathAliases(workspaceRoot, map);
  } catch (e) {
    log(`scanPathAliases failed: ${e.message}`);
  }

  return map;
}

/**
 * Locate the @npm external repo directory under `output_base/external/`.
 * Handles the three known canonical name forms:
 *   - +npm+npm       (bzlmod, typical)
 *   - npm            (legacy WORKSPACE)
 *   - *npm*          (any variant containing "npm" with a BUILD.bazel)
 *
 * Returns the directory path, or null if not found.
 *
 * @param {string} outputBase
 * @returns {string | null}
 */
function findNpmExternalDir(outputBase) {
  const externalDir = path.join(outputBase, 'external');
  if (!fs.existsSync(externalDir)) return null;

  // Quick check for well-known names first.
  for (const candidate of [
    path.join(externalDir, '+npm+npm'),
    path.join(externalDir, 'npm'),
  ]) {
    if (
      fs.existsSync(candidate) &&
      fs.existsSync(path.join(candidate, 'BUILD.bazel'))
    ) {
      // Verify it looks like our npm repo (contains ts_npm_package rules).
      try {
        const content = fs.readFileSync(
          path.join(candidate, 'BUILD.bazel'),
          'utf8'
        );
        if (content.includes('ts_npm_package')) {
          return candidate;
        }
      } catch (_) {
        // ignore
      }
    }
  }

  // Fallback: scan external/ for any directory whose name contains "npm" and
  // whose BUILD.bazel contains ts_npm_package rules.
  try {
    const entries = fs.readdirSync(externalDir);
    for (const entry of entries) {
      if (!entry.includes('npm')) continue;
      const candidate = path.join(externalDir, entry);
      const buildPath = path.join(candidate, 'BUILD.bazel');
      if (!fs.existsSync(buildPath)) continue;
      try {
        const content = fs.readFileSync(buildPath, 'utf8');
        if (content.includes('ts_npm_package')) {
          return candidate;
        }
      } catch (_) {
        // ignore
      }
    }
  } catch (_) {
    // ignore
  }

  return null;
}

/**
 * Scan the @npm external repo directory and populate `map` with
 * package-name → absolute .d.ts path entries.
 *
 * The @npm repo layout (generated by npm_translate_lock) is:
 *   <npmDir>/<pkg>__<version>/  — extracted package tarballs
 *
 * We read the BUILD.bazel to find ts_npm_package() stanzas (which carry
 * the authoritative exports_types field), then fall back to reading
 * package.json directly.
 *
 * @param {string} npmDir
 * @param {Record<string, string>} map
 */
function scanNpmPackages(npmDir, map) {
  const buildPath = path.join(npmDir, 'BUILD.bazel');

  // Prefer the BUILD.bazel parsing approach (same logic as refresh_tsconfig.sh)
  // because it gives us the authoritative exports_types path.
  if (fs.existsSync(buildPath)) {
    try {
      const content = fs.readFileSync(buildPath, 'utf8');
      const parsed = parseTsNpmPackageStanzas(content, npmDir);

      for (const [pkgName, dtsPath] of Object.entries(parsed)) {
        if (fs.existsSync(dtsPath)) {
          map[pkgName] = dtsPath;
          log(`npm (BUILD.bazel): ${pkgName} → ${dtsPath}`);
        }
      }
      return; // BUILD.bazel approach succeeded
    } catch (e) {
      log(`BUILD.bazel parse failed: ${e.message} — falling back to package.json scan`);
    }
  }

  // Fallback: scan package.json files directly (for repos without BUILD.bazel).
  try {
    const entries = fs.readdirSync(npmDir);
    for (const entry of entries) {
      const pkgJsonPath = path.join(npmDir, entry, 'package.json');
      if (!fs.existsSync(pkgJsonPath)) continue;

      try {
        const pkgJson = JSON.parse(fs.readFileSync(pkgJsonPath, 'utf8'));
        const name = pkgJson.name;
        if (!name) continue;

        // Skip if already mapped (BUILD.bazel approach takes precedence).
        if (map[name]) continue;

        const dtsPath = resolvePackageDts(pkgJson, path.join(npmDir, entry));
        if (dtsPath) {
          map[name] = dtsPath;
          log(`npm (package.json): ${name} → ${dtsPath}`);
        }
      } catch (_) {
        // Malformed package — skip.
      }
    }
  } catch (_) {
    // ignore
  }
}

/**
 * Parse ts_npm_package() stanzas from a BUILD.bazel string.
 * Returns a map of package_name → absolute .d.ts path.
 *
 * @param {string} content  - Contents of the BUILD.bazel file.
 * @param {string} npmDir   - Absolute path to the @npm external repo root.
 * @returns {Record<string, string>}
 */
function parseTsNpmPackageStanzas(content, npmDir) {
  const result = {};
  const stanzaMarker = 'ts_npm_package(';
  let i = 0;

  while (true) {
    const start = content.indexOf(stanzaMarker, i);
    if (start === -1) break;

    // Find the matching closing paren by tracking depth.
    let depth = 0;
    let j = start + stanzaMarker.length - 1; // position of opening "("
    while (j < content.length) {
      if (content[j] === '(') depth++;
      else if (content[j] === ')') {
        depth--;
        if (depth === 0) break;
      }
      j++;
    }

    const stanza = content.slice(start, j + 1);
    i = j + 1;

    const pkgNameMatch = stanza.match(/\bpackage_name\s*=\s*"([^"]+)"/);
    const exportsTypesMatch = stanza.match(/\bexports_types\s*=\s*"([^"]+)"/);
    const pkgDirMatch = stanza.match(/\bpackage_dir\s*=\s*"([^"]+)"/);
    const isTypesMatch = stanza.match(/\bis_types_package\s*=\s*(True|False)/);

    if (!pkgNameMatch) continue;

    const pkgName = pkgNameMatch[1];
    const isTypes = isTypesMatch ? isTypesMatch[1] === 'True' : false;

    // Skip @types/* packages — they are paired to runtime packages.
    if (isTypes) continue;

    // First occurrence wins.
    if (result[pkgName]) continue;

    let dtsRel = exportsTypesMatch ? exportsTypesMatch[1] : null;

    if (!dtsRel && pkgDirMatch) {
      // Derive package subdir from package_dir field.
      let pkgSubdir = pkgDirMatch[1];
      if (pkgSubdir.endsWith('/package.json')) {
        pkgSubdir = pkgSubdir.slice(0, -'/package.json'.length);
      }
      const pkgJsonPath = path.join(npmDir, pkgSubdir, 'package.json');
      if (fs.existsSync(pkgJsonPath)) {
        try {
          const pkgJson = JSON.parse(fs.readFileSync(pkgJsonPath, 'utf8'));
          const typesField = pkgJson.types || pkgJson.typings || '';
          if (typesField) {
            const normalized = typesField.replace(/^\.\//, '');
            dtsRel = `${pkgSubdir}/${normalized}`;
          } else {
            const idx = path.join(npmDir, pkgSubdir, 'index.d.ts');
            if (fs.existsSync(idx)) {
              dtsRel = `${pkgSubdir}/index.d.ts`;
            }
          }
        } catch (_) {
          // ignore
        }
      }
    }

    if (!dtsRel) continue;

    const absDts = path.join(npmDir, dtsRel);
    if (
      absDts.endsWith('.d.ts') ||
      absDts.endsWith('.d.mts') ||
      absDts.endsWith('.d.cts')
    ) {
      result[pkgName] = absDts;
    }
  }

  return result;
}

/**
 * Resolve the primary .d.ts entry point for a package given its package.json
 * and absolute directory path.
 *
 * @param {object} pkgJson  - Parsed package.json object.
 * @param {string} pkgDir   - Absolute path to the package directory.
 * @returns {string | null}
 */
function resolvePackageDts(pkgJson, pkgDir) {
  // Priority 1: exports['.']['types']
  if (pkgJson.exports && typeof pkgJson.exports === 'object') {
    const main = pkgJson.exports['.'];
    if (main) {
      const typesTarget =
        typeof main === 'object'
          ? main.types || main.import || main.default
          : main;
      if (typeof typesTarget === 'string') {
        const resolved = path.resolve(pkgDir, typesTarget);
        if (isDtsFile(resolved) && fs.existsSync(resolved)) {
          return resolved;
        }
      }
    }
  }

  // Priority 2: top-level "types" / "typings" field
  const typesField = pkgJson.types || pkgJson.typings;
  if (typesField) {
    const resolved = path.resolve(pkgDir, typesField);
    if (isDtsFile(resolved) && fs.existsSync(resolved)) {
      return resolved;
    }
  }

  // Priority 3: index.d.ts at package root
  const idx = path.join(pkgDir, 'index.d.ts');
  if (fs.existsSync(idx)) {
    return idx;
  }

  return null;
}

/**
 * @param {string} p
 * @returns {boolean}
 */
function isDtsFile(p) {
  return p.endsWith('.d.ts') || p.endsWith('.d.mts') || p.endsWith('.d.cts');
}

/**
 * Scan an internal ts_compile package and add a resolution entry.
 *
 * Prefers .d.ts in bazel-bin (post-build) over .ts source (pre-build).
 *
 * @param {string} pkg     - Package path relative to workspace root, e.g. "src/utils".
 * @param {string} srcDir  - Absolute path to the package source directory.
 * @param {string} binDir  - Absolute path to the package in bazel-bin.
 * @param {Record<string, string>} map
 */
function scanPackageForResolution(pkg, srcDir, binDir, map) {
  for (const filename of ['index.d.ts', 'index.ts', 'index.tsx']) {
    const binCandidate = path.join(binDir, filename);
    if (fs.existsSync(binCandidate)) {
      map[pkg] = binCandidate;
      log(`internal (bin): ${pkg} → ${binCandidate}`);
      return;
    }
    const srcCandidate = path.join(srcDir, filename);
    if (fs.existsSync(srcCandidate)) {
      map[pkg] = srcCandidate;
      log(`internal (src): ${pkg} → ${srcCandidate}`);
      return;
    }
  }
}

/**
 * Walk BUILD files in the workspace and extract # gazelle:ts_path_alias
 * directives.  Each directive maps an alias prefix to a source directory.
 *
 * The alias is stored with the "__alias__" prefix so the main thread can
 * distinguish it from direct module-name mappings.
 *
 * Format:  # gazelle:ts_path_alias <alias_prefix> <workspace-relative-dir>
 *
 * @param {string} root
 * @param {Record<string, string>} map
 */
function scanPathAliases(root, map) {
  const re = /^\s*#\s*gazelle:ts_path_alias\s+(\S+)\s+(\S+)/;

  // Walk the workspace tree, stopping at nested workspace boundaries.
  const BOUNDARY_FILES = new Set(['MODULE.bazel', 'WORKSPACE', 'WORKSPACE.bazel']);
  const PRUNE_DIRS = new Set([
    'node_modules', 'dist', 'build', '.next', '.nuxt',
  ]);

  function walk(dir, isRoot) {
    let entries;
    try {
      entries = fs.readdirSync(dir, { withFileTypes: true });
    } catch (_) {
      return;
    }

    // Check for child workspace boundary (skip everything except root).
    if (!isRoot) {
      const isBoundary = entries.some(
        (e) => e.isFile() && BOUNDARY_FILES.has(e.name)
      );
      if (isBoundary) return;
    }

    for (const entry of entries) {
      if (entry.name.startsWith('.') || entry.name.startsWith('bazel-')) continue;
      if (PRUNE_DIRS.has(entry.name)) continue;

      if (entry.isFile() && (entry.name === 'BUILD.bazel' || entry.name === 'BUILD')) {
        const filePath = path.join(dir, entry.name);
        try {
          const lines = fs.readFileSync(filePath, 'utf8').split('\n');
          for (const line of lines) {
            const m = line.match(re);
            if (!m) continue;
            const aliasPrefix = m[1]; // e.g. "@/"
            const aliasDir = m[2];    // e.g. "src/"

            // Validate: only safe characters.
            if (!/^[A-Za-z0-9@/_.*-]+$/.test(aliasPrefix)) continue;
            if (!/^[A-Za-z0-9@/_.*-]+$/.test(aliasDir)) continue;

            const key = `__alias__${aliasPrefix.replace(/\/$/, '')}/`;
            if (map[key]) continue; // First occurrence wins.

            const absDir = path.join(workspaceRoot, aliasDir.replace(/\/$/, ''));
            map[key] = absDir;
            log(`path alias: ${aliasPrefix} → ${absDir}`);
          }
        } catch (_) {
          // ignore unreadable BUILD files
        }
      } else if (entry.isDirectory()) {
        walk(path.join(dir, entry.name), false);
      }
    }
  }

  walk(root, true);
}

// ── Initial build ─────────────────────────────────────────────────────────────

log(`starting in workspace ${workspaceRoot}`);

const initialMap = buildResolutionMap();
const initialEntries = Object.keys(initialMap).length;
log(`initial resolution map: ${initialEntries} entries`);

parentPort.postMessage({ type: 'resolution-map', data: initialMap });

// ── File-system watchers ──────────────────────────────────────────────────────
// Rebuild the map when key files change.  We use Node's built-in fs.watch
// (no chokidar dependency).  The rebuild is debounced to avoid thrashing.

let rebuildTimer = null;

function scheduleRebuild(delay) {
  if (rebuildTimer) clearTimeout(rebuildTimer);
  rebuildTimer = setTimeout(() => {
    rebuildTimer = null;
    log('rebuilding resolution map...');
    try {
      const newMap = buildResolutionMap();
      log(`rebuilt: ${Object.keys(newMap).length} entries`);
      parentPort.postMessage({ type: 'resolution-map', data: newMap });
    } catch (e) {
      log(`rebuild failed: ${e.message}`);
    }
  }, delay);
}

// Watch root-level BUILD files and pnpm-lock.yaml (most likely to change when
// packages are added or removed).
const rootWatchPaths = [
  path.join(workspaceRoot, 'BUILD.bazel'),
  path.join(workspaceRoot, 'BUILD'),
  path.join(workspaceRoot, 'pnpm-lock.yaml'),
];

for (const watchPath of rootWatchPaths) {
  if (!fs.existsSync(watchPath)) continue;
  try {
    fs.watch(watchPath, { persistent: false }, () => {
      log(`file changed: ${watchPath}`);
      scheduleRebuild(1000);
    });
  } catch (_) {
    // fs.watch can fail on some systems/filesystems — ignore.
  }
}

// Watch bazel-bin for new .d.ts files (generated after `bazel build`).
// Use recursive watch so nested packages are covered.
const bazelBin = path.join(workspaceRoot, 'bazel-bin');
if (fs.existsSync(bazelBin)) {
  try {
    fs.watch(bazelBin, { recursive: true, persistent: false }, (_event, filename) => {
      if (filename && filename.endsWith('.d.ts')) {
        log(`bazel-bin changed: ${filename}`);
        scheduleRebuild(500);
      }
    });
  } catch (_) {
    // Recursive watch is not supported on all platforms — ignore.
  }
}
