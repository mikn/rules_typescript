/**
 * tsserver-hook-worker.js — Background worker for the Bazel-aware tsserver hook.
 *
 * Runs in a worker thread (spawned by tsserver-hook.js).
 * Builds a resolution map from:
 *   1. The npm packages, ts_compile packages and path aliases named in
 *      .bazel/tsserver-hook-data.json, which `bazel run //:refresh_tsconfig`
 *      writes from the build graph.
 *   2. Path-alias directives (# gazelle:ts_path_alias) in BUILD files, for
 *      directives added since the last refresh.
 *
 * Sends the map to the main thread via postMessage, then sets up file-system
 * watches to rebuild the map when that data, a BUILD file or pnpm-lock.yaml
 * changes.
 *
 * Design constraints:
 *   - Zero npm dependencies (Node.js builtins only).
 *   - Never runs Bazel: this is an editor process, and asking the Bazel server
 *     anything from here would block on the lock a build holds. Everything
 *     Bazel knows arrives through the generated data file.
 *   - Must degrade gracefully when that file is absent or stale.
 */

'use strict';

const { parentPort, workerData } = require('worker_threads');
const path = require('path');
const fs = require('fs');

const { workspaceRoot, dataFile: providedDataFile } = workerData;

// Where `bazel run //:refresh_tsconfig` installs the graph data, workspace-relative.
const HOOK_DATA = '.bazel/tsserver-hook-data.json';

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
  const data = readHookData();

  if (!data) {
    log(
      `no hook data at ${path.join(workspaceRoot, HOOK_DATA)} — ` +
        'run `bazel run //:refresh_tsconfig` to generate it'
    );
  } else {
    // Step 1: npm packages, installed in the workspace by refresh_tsconfig.
    // Only the packages the aspect reached are listed, which is the same set
    // the generated tsconfig.json exposes.
    const npmDir = path.join(workspaceRoot, data.npmDir || '');
    let resolved = 0;
    for (const pkg of data.npmPackages || []) {
      if (!pkg || !pkg.name || map[pkg.name]) continue;
      const dtsPath = resolveInstalledPackage(npmDir, pkg);
      if (dtsPath) {
        map[pkg.name] = dtsPath;
        resolved += 1;
        log(`npm: ${pkg.name} → ${dtsPath}`);
      } else {
        log(`npm: ${pkg.name} has no declarations under ${npmDir}`);
      }
    }
    log(`npm: resolved ${resolved} of ${(data.npmPackages || []).length} packages`);

    // Step 2: internal ts_compile packages.
    for (const pkg of data.packages || []) {
      const srcDir = path.join(workspaceRoot, pkg);
      const binDir = path.join(workspaceRoot, 'bazel-bin', pkg);
      scanPackageForResolution(pkg, srcDir, binDir, map);
    }

    // Step 3: the path aliases the build graph carries.
    for (const alias of data.aliases || []) {
      if (!alias || !alias.prefix || !alias.dir) continue;
      const key = `__alias__${alias.prefix.replace(/\/$/, '')}/`;
      if (map[key]) continue;
      map[key] = path.join(workspaceRoot, alias.dir.replace(/\/$/, ''));
      log(`path alias: ${alias.prefix} → ${map[key]}`);
    }
  }

  // Step 4: path aliases from BUILD files, which cover directives added since
  // the last refresh. The generated data wins: it is what the build resolves.
  try {
    scanPathAliases(workspaceRoot, map);
  } catch (e) {
    log(`scanPathAliases failed: ${e.message}`);
  }

  return map;
}

/**
 * The graph data `bazel run //:refresh_tsconfig` writes, or null.
 *
 * @returns {object | null}
 */
function readHookData() {
  const candidates = [
    providedDataFile,
    process.env.TSSERVER_HOOK_DATA,
    path.join(workspaceRoot, HOOK_DATA),
  ].filter(Boolean);

  for (const candidate of candidates) {
    try {
      return JSON.parse(fs.readFileSync(candidate, 'utf8'));
    } catch (e) {
      log(`hook data at ${candidate} unusable: ${e.message}`);
    }
  }
  return null;
}

/**
 * The .d.ts one installed npm package resolves to, or null.
 *
 * `entry` is what the aspect knew: the package's own exports["."].types when it
 * declares one, otherwise the directory whose package.json names the rest.
 *
 * @param {string} npmDir - Absolute path to the installed npm tree.
 * @param {{name: string, entry: string, isFile: boolean}} pkg
 * @returns {string | null}
 */
function resolveInstalledPackage(npmDir, pkg) {
  const target = path.join(npmDir, pkg.name, pkg.entry || '');
  if (pkg.isFile) {
    return isDtsFile(target) && fs.existsSync(target) ? target : null;
  }
  let pkgJson = {};
  try {
    pkgJson = JSON.parse(fs.readFileSync(path.join(target, 'package.json'), 'utf8'));
  } catch (_) {
    // No package.json: resolvePackageDts still tries index.d.ts.
  }
  return resolvePackageDts(pkgJson, target);
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

// Watch the generated graph data, the root-level BUILD files and
// pnpm-lock.yaml: between them, everything that changes what resolves.
const rootWatchPaths = [
  providedDataFile || path.join(workspaceRoot, HOOK_DATA),
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
