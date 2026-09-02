/**
 * tsserver-hook-worker.js — Background worker for the Bazel-aware tsserver hook.
 *
 * Runs in a worker thread (spawned by tsserver-hook.js).
 * Builds a resolution map from:
 *   1. The npm packages, ts_compile packages, declared module names and path
 *      aliases named in .bazel/tsserver-hook-data.json, which
 *      `bazel run //:refresh_tsconfig` writes from the build graph.
 *   2. The .tsconfig-fragment.json files tsconfig_aspect's `ide_fragments`
 *      output group writes into bazel-out, one per target. A rule's `deps` obey
 *      visibility and an aspect's edges do not, so these cover the targets the
 *      data file cannot name -- and they are optional: without the .bazelrc
 *      lines that request the group there are none, and (1) is the whole map.
 *   3. Path-alias directives (# gazelle:ts_path_alias) in BUILD files, for
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
 *     Bazel knows arrives through files a build already wrote.
 *   - Must degrade gracefully when any of them is absent or stale. Nothing
 *     enters the map without the path it names existing on disk, which is also
 *     what keeps a fragment left behind by a deleted target from being wrong.
 */

'use strict';

const { parentPort, workerData } = require('worker_threads');
const path = require('path');
const fs = require('fs');

const { workspaceRoot, dataFile: providedDataFile } = workerData;

// Where `bazel run //:refresh_tsconfig` installs the graph data, workspace-relative.
const HOOK_DATA = '.bazel/tsserver-hook-data.json';

// One per target, written by tsconfig_aspect's `ide_fragments` output group.
const FRAGMENT_SUFFIX = '.tsconfig-fragment.json';
const FRAGMENT_FORMAT = 'tsconfig-fragment-v1';

// The rule attribute's default, so fragment npm entries still resolve when no
// data file says where the declarations were installed.
const DEFAULT_NPM_DIR = '.bazel/npm';

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
  // `npm_dir = ""` on the rule is a deliberate opt-out of npm entries, so an
  // empty string in the data file is null here, not the default.
  const configured = data ? data.npmDir : DEFAULT_NPM_DIR;
  const npmDir = configured ? path.join(workspaceRoot, configured) : null;

  if (!data) {
    log(
      `no hook data at ${path.join(workspaceRoot, HOOK_DATA)} — ` +
        'run `bazel run //:refresh_tsconfig` to generate it'
    );
  } else {
    // Step 1: npm packages, installed in the workspace by refresh_tsconfig.
    // Only the packages the aspect reached are listed, which is the same set
    // the generated tsconfig.json exposes.
    let resolved = 0;
    for (const pkg of npmDir ? data.npmPackages || [] : []) {
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

    // Step 2b: the bare specifiers targets declared with `module_name`. Same
    // directories as step 2, under the name an import actually writes.
    for (const module of data.modules || []) {
      if (!module || !module.name || !module.package || map[module.name]) continue;
      scanPackageForResolution(
        module.name,
        path.join(workspaceRoot, module.package),
        path.join(workspaceRoot, 'bazel-bin', module.package),
        map
      );
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

  // Step 4: the aspect's per-target fragments, which reach the targets no rule
  // can name. They augment what the data file already resolved, never replace
  // it, and there are none at all until a build requests the output group.
  let tree = { packages: [], aliases: [] };
  try {
    tree = walkWorkspace(workspaceRoot);
  } catch (e) {
    log(`workspace walk failed: ${e.message}`);
  }
  mergeFragments(readFragments(tree.packages), npmDir, map);

  // Step 5: path aliases from BUILD files, which cover directives added since
  // the last refresh. The graph wins over them: it is what the build resolves.
  for (const alias of tree.aliases) {
    const key = `__alias__${alias.prefix}/`;
    if (map[key]) continue;
    map[key] = path.join(workspaceRoot, alias.dir);
    log(`path alias (BUILD): ${alias.prefix} → ${map[key]}`);
  }

  return map;
}

// ── Fragments ────────────────────────────────────────────────────────────────

/**
 * Every `<config>/bin` a build could have written fragments into.
 *
 * One target built in two configurations writes two fragments under two
 * different config directories, so the roots are deduped by real path and
 * sorted: the merge is then the same whatever order the filesystem lists them
 * in.
 *
 * @returns {string[]}
 */
function fragmentRoots() {
  const roots = new Set();
  const add = (p) => {
    try {
      if (fs.statSync(p).isDirectory()) roots.add(fs.realpathSync(p));
    } catch (_) {
      // Not a directory the convenience symlinks produced here.
    }
  };

  add(path.join(workspaceRoot, 'bazel-bin'));
  const outDir = path.join(workspaceRoot, 'bazel-out');
  let configs = [];
  try {
    configs = fs.readdirSync(outDir);
  } catch (_) {
    // No bazel-out symlink: --experimental_convenience_symlinks=ignore, or no
    // build yet.
  }
  for (const config of configs) {
    add(path.join(outDir, config, 'bin'));
  }
  return [...roots].sort();
}

/**
 * The fragments found under every config root, one per target label.
 *
 * Discovery is rooted in the source tree rather than in a recursive walk of
 * bazel-out: a fragment lives at `<config>/bin/<package>/<target>` +
 * FRAGMENT_SUFFIX, so `packageDirs` is the complete list of directories to look
 * in, and a fragment whose package has since been deleted is never opened.
 *
 * @param {string[]} packageDirs - Workspace-relative dirs holding a BUILD file.
 * @returns {Array<{label: string, packages: string[], modules: Array<{name: string, package: string}>, aliases: Array<{prefix: string, dir: string}>, npm: Array<{name: string, dir: string, version: string, entry: string, isFile: boolean}>}>}
 */
function readFragments(packageDirs) {
  const seen = new Set();
  const fragments = [];
  let files = 0;

  for (const root of fragmentRoots()) {
    for (const pkg of packageDirs) {
      let names;
      try {
        names = fs.readdirSync(path.join(root, pkg));
      } catch (_) {
        continue;
      }
      for (const name of names.sort()) {
        if (!name.endsWith(FRAGMENT_SUFFIX)) continue;
        files += 1;
        const fragment = parseFragment(path.join(root, pkg, name));
        // The same label under a second configuration says the same thing about
        // the source tree, and counting it twice would make the merge depend on
        // how many configurations happen to be in bazel-out.
        if (!fragment || seen.has(fragment.label)) continue;
        seen.add(fragment.label);
        fragments.push(fragment);
      }
    }
  }

  log(`fragments: ${fragments.length} labels from ${files} files`);
  return fragments;
}

/**
 * One fragment file: a JSON object per line, the first naming the format and the
 * target label. Returns null for anything that is not a fragment this version
 * understands.
 *
 * @param {string} file
 * @returns {object | null}
 */
function parseFragment(file) {
  let lines;
  try {
    lines = fs.readFileSync(file, 'utf8').split('\n');
  } catch (e) {
    log(`fragment ${file} unreadable: ${e.message}`);
    return null;
  }

  const fragment = { label: null, packages: [], modules: [], aliases: [], npm: [] };
  for (const line of lines) {
    if (!line.trim()) continue;
    let record;
    try {
      record = JSON.parse(line);
    } catch (e) {
      log(`fragment ${file} is not JSON per line: ${e.message}`);
      return null;
    }
    if (record.format !== undefined) {
      if (record.format !== FRAGMENT_FORMAT || typeof record.label !== 'string') {
        log(`fragment ${file} has format ${record.format}, want ${FRAGMENT_FORMAT}`);
        return null;
      }
      fragment.label = record.label;
    } else if (typeof record.package === 'string') {
      fragment.packages.push(record.package);
      if (typeof record.module === 'string' && record.module) {
        fragment.modules.push({ name: record.module, package: record.package });
      }
    } else if (typeof record.alias === 'string' && typeof record.dir === 'string') {
      fragment.aliases.push({
        prefix: record.alias.replace(/\/$/, ''),
        dir: record.dir.replace(/\/$/, ''),
      });
    } else if (typeof record.npm === 'string') {
      fragment.npm.push({
        name: record.npm,
        dir: typeof record.dir === 'string' && record.dir ? record.dir : record.npm,
        version: String(record.version || ''),
        entry: record.entry || '',
        isFile: !!record.file,
      });
    }
  }

  if (!fragment.label) {
    log(`fragment ${file} names no label`);
    return null;
  }
  return fragment;
}

const byKey = ([a], [b]) => (a < b ? -1 : a > b ? 1 : 0);

/**
 * Fold the fragments into `map`, leaving every key the data file already
 * resolved alone.
 *
 * @param {object[]} fragments
 * @param {string | null} npmDir - The installed npm tree, or null when npm_dir is off.
 * @param {Record<string, string>} map
 */
function mergeFragments(fragments, npmDir, map) {
  const packages = new Set();
  const modules = new Map();
  const aliases = new Map();
  const npm = new Map();

  for (const fragment of fragments) {
    for (const pkg of fragment.packages) packages.add(pkg);
    for (const module of fragment.modules) {
      if (!modules.has(module.name)) modules.set(module.name, module.package);
    }
    for (const alias of fragment.aliases) {
      if (alias.prefix && alias.dir && !aliases.has(alias.prefix)) {
        aliases.set(alias.prefix, alias.dir);
      }
    }
    for (const entry of fragment.npm) {
      const chosen = npm.get(entry.name);
      // Two versions of one name would fight over the same directory under
      // npmDir. The generated tsconfig gives the whole name to the lowest
      // version, so the hook has to agree or the two disagree about one import.
      if (!chosen || entry.version < chosen.version) npm.set(entry.name, entry);
    }
  }

  for (const [name, entry] of npmDir ? [...npm].sort(byKey) : []) {
    if (map[name]) continue;
    // Only the first-party half of a fragment is self-contained. An npm .d.ts
    // lives in an external repository no workspace-relative path reaches, so it
    // resolves here only if `bazel run //:refresh_tsconfig` installed it -- and
    // that target's own deps decide what it installs.
    const dtsPath = resolveInstalledPackage(npmDir, {
      name,
      dir: entry.dir,
      entry: entry.entry,
      isFile: entry.isFile,
    });
    if (!dtsPath) continue;
    map[name] = dtsPath;
    log(`fragment npm: ${name} → ${dtsPath}`);
  }

  for (const pkg of [...packages].sort()) {
    if (map[pkg]) continue;
    scanPackageForResolution(
      pkg,
      path.join(workspaceRoot, pkg),
      path.join(workspaceRoot, 'bazel-bin', pkg),
      map
    );
  }

  for (const [name, pkg] of [...modules].sort(byKey)) {
    if (map[name]) continue;
    scanPackageForResolution(
      name,
      path.join(workspaceRoot, pkg),
      path.join(workspaceRoot, 'bazel-bin', pkg),
      map
    );
  }

  for (const [prefix, dir] of [...aliases].sort(byKey)) {
    const key = `__alias__${prefix}/`;
    if (map[key]) continue;
    const absDir = path.join(workspaceRoot, dir);
    // The data file is rewritten whole on every refresh; a fragment is not, so
    // a renamed alias leaves the old one in bazel-out until that target is next
    // built. A directory that is gone is how that shows up.
    if (!fs.existsSync(absDir)) {
      log(`fragment alias: ${prefix} → ${absDir} (gone, skipped)`);
      continue;
    }
    map[key] = absDir;
    log(`fragment alias: ${prefix} → ${absDir}`);
  }
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
 * `dir` is the installed package the files sit under, which is not `name` for a
 * `@types/*` package: it answers the name it types and is installed under its
 * own. Absent on an entry written before that distinction existed, where the
 * two were always the same.
 *
 * @param {string} npmDir - Absolute path to the installed npm tree.
 * @param {{name: string, dir?: string, entry: string, isFile: boolean}} pkg
 * @returns {string | null}
 */
function resolveInstalledPackage(npmDir, pkg) {
  const target = path.join(npmDir, pkg.dir || pkg.name, pkg.entry || '');
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
 * @param {string} pkg     - The map key: a package path relative to the
 *                           workspace root, e.g. "src/utils", or the bare
 *                           specifier a target declared with `module_name`.
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
 * One walk of the source tree, for the two things it is the authority on: the
 * # gazelle:ts_path_alias directives in BUILD files, and where the Bazel
 * packages are.
 *
 * Directive format:  # gazelle:ts_path_alias <alias_prefix> <workspace-relative-dir>
 *
 * The package list is what makes fragment discovery cheap and self-cleaning: a
 * fragment can only sit under a package directory, so nothing else in bazel-out
 * has to be read, and a package that no longer exists in the source tree is not
 * looked in.
 *
 * @param {string} root
 * @returns {{packages: string[], aliases: Array<{prefix: string, dir: string}>}}
 */
function walkWorkspace(root) {
  const re = /^\s*#\s*gazelle:ts_path_alias\s+(\S+)\s+(\S+)/;
  const BOUNDARY_FILES = new Set(['MODULE.bazel', 'WORKSPACE', 'WORKSPACE.bazel']);
  const PRUNE_DIRS = new Set(['node_modules', 'dist', 'build', '.next', '.nuxt']);
  const BUILD_FILES = new Set(['BUILD.bazel', 'BUILD']);

  const packages = [];
  const aliases = [];
  const seenPrefix = new Set();

  function readDirectives(filePath) {
    let lines;
    try {
      lines = fs.readFileSync(filePath, 'utf8').split('\n');
    } catch (_) {
      return;
    }
    for (const line of lines) {
      const m = line.match(re);
      if (!m) continue;
      const prefix = m[1]; // e.g. "@/"
      const dir = m[2]; // e.g. "src/"
      // Only safe characters: this is the one input that is text rather than
      // graph, so a prefix gazelle would accept can still be refused here.
      if (!/^[A-Za-z0-9@/_.*-]+$/.test(prefix)) continue;
      if (!/^[A-Za-z0-9@/_.*-]+$/.test(dir)) continue;
      const stripped = prefix.replace(/\/$/, '');
      if (seenPrefix.has(stripped)) continue; // First occurrence wins.
      seenPrefix.add(stripped);
      aliases.push({ prefix: stripped, dir: dir.replace(/\/$/, '') });
    }
  }

  function walk(dir, isRoot) {
    let entries;
    try {
      entries = fs.readdirSync(dir, { withFileTypes: true });
    } catch (_) {
      return;
    }

    // A child workspace's directives and packages are that workspace's, not
    // this one's.
    if (!isRoot && entries.some((e) => e.isFile() && BOUNDARY_FILES.has(e.name))) {
      return;
    }

    // Sorted, so which BUILD file wins a repeated alias prefix does not depend
    // on the order the filesystem happens to list directories in.
    entries.sort((a, b) => (a.name < b.name ? -1 : a.name > b.name ? 1 : 0));

    let isPackage = false;
    for (const entry of entries) {
      if (entry.name.startsWith('.') || entry.name.startsWith('bazel-')) continue;
      if (PRUNE_DIRS.has(entry.name)) continue;

      if (entry.isFile() && BUILD_FILES.has(entry.name)) {
        isPackage = true;
        readDirectives(path.join(dir, entry.name));
      } else if (entry.isDirectory()) {
        walk(path.join(dir, entry.name), false);
      }
    }
    if (isPackage) packages.push(path.relative(root, dir));
  }

  walk(root, true);
  return { packages: packages.sort(), aliases };
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

// Watch bazel-bin for the two things a `bazel build` adds: .d.ts files, and the
// aspect's fragments. Use recursive watch so nested packages are covered.
const bazelBin = path.join(workspaceRoot, 'bazel-bin');
if (fs.existsSync(bazelBin)) {
  try {
    fs.watch(bazelBin, { recursive: true, persistent: false }, (_event, filename) => {
      if (filename && (filename.endsWith('.d.ts') || filename.endsWith(FRAGMENT_SUFFIX))) {
        log(`bazel-bin changed: ${filename}`);
        scheduleRebuild(500);
      }
    });
  } catch (_) {
    // Recursive watch is not supported on all platforms — ignore.
  }
}
