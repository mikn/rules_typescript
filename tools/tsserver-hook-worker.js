// The tsserver hook's worker (Node builtins only): the resolution map from
// .bazel/tsserver-hook-data.json and the aspect's fragments, off-thread.

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
    // Step 1: internal ts_compile packages.
    for (const pkg of data.packages || []) {
      const srcDir = path.join(workspaceRoot, pkg);
      const binDir = path.join(workspaceRoot, 'bazel-bin', pkg);
      scanPackageForResolution(pkg, srcDir, binDir, map);
    }
  }

  // Step 2: the aspect's fragments augment what the data file resolved, never
  // replace it; there are none until a build requests the output group.
  let packages = [];
  try {
    packages = walkWorkspace(workspaceRoot);
  } catch (e) {
    log(`workspace walk failed: ${e.message}`);
  }
  mergeFragments(readFragments(packages), map);

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

// The fragments under every config root, one per label. Discovery is rooted in
// the source tree's packages, so a deleted package's fragment is never opened.
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

  const fragment = { label: null, packages: [] };
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
    }
  }

  if (!fragment.label) {
    log(`fragment ${file} names no label`);
    return null;
  }
  return fragment;
}

/**
 * Fold the fragments into `map`, leaving every key the data file already
 * resolved alone.
 *
 * @param {object[]} fragments
 * @param {Record<string, string>} map
 */
function mergeFragments(fragments, map) {
  const packages = new Set();
  for (const fragment of fragments) {
    for (const pkg of fragment.packages) packages.add(pkg);
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

// One walk of the source tree for the Bazel packages: a fragment can only sit
// under a package directory, so nothing else in bazel-out is read.
function walkWorkspace(root) {
  const BOUNDARY_FILES = new Set(['MODULE.bazel', 'WORKSPACE', 'WORKSPACE.bazel']);
  const PRUNE_DIRS = new Set(['node_modules', 'dist', 'build', '.next', '.nuxt']);
  const BUILD_FILES = new Set(['BUILD.bazel', 'BUILD']);

  const packages = [];

  function walk(dir, isRoot) {
    let entries;
    try {
      entries = fs.readdirSync(dir, { withFileTypes: true });
    } catch (_) {
      return;
    }

    // A child workspace's packages are that workspace's, not this one's.
    if (!isRoot && entries.some((e) => e.isFile() && BOUNDARY_FILES.has(e.name))) {
      return;
    }

    let isPackage = false;
    for (const entry of entries) {
      if (entry.name.startsWith('.') || entry.name.startsWith('bazel-')) continue;
      if (PRUNE_DIRS.has(entry.name)) continue;

      if (entry.isFile() && BUILD_FILES.has(entry.name)) {
        isPackage = true;
      } else if (entry.isDirectory()) {
        walk(path.join(dir, entry.name), false);
      }
    }
    if (isPackage) packages.push(path.relative(root, dir));
  }

  walk(root, true);
  return packages.sort();
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

// Watch the generated graph data: with bazel-bin below, everything that changes
// what resolves.
const dataFile = providedDataFile || path.join(workspaceRoot, HOOK_DATA);
if (fs.existsSync(dataFile)) {
  try {
    fs.watch(dataFile, { persistent: false }, () => {
      log(`file changed: ${dataFile}`);
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
