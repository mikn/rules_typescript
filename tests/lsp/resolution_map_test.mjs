/**
 * resolution_map_test.mjs — the worker's map of what this repo's own aspect
 * generated.
 *
 *   node resolution_map_test.mjs <tsserver-hook-worker.js> <workspace-root>
 *
 * The workspace root is the scratch tree //tests/lsp:test_resolution_map staged
 * by running refresh_tsconfig's copier into it, so the npm entry points under
 * test are the real ones from this repo's lockfile -- which is what the fixture
 * in worker_map_test.mjs cannot cover.
 *
 * Only npm packages are asserted: the scratch tree holds no .ts sources, so the
 * internal-package half of the map is empty by construction, and the alias half
 * needs directives this repo does not have outside its child workspaces. Both
 * are covered by //tests/lsp:test_worker_map.
 */

import { Worker } from 'node:worker_threads';
import fs from 'node:fs';
import path from 'node:path';

const [workerPath, workspaceRoot] = process.argv.slice(2);

// From this repo's pnpm-lock.yaml, via the ts_compile targets //:refresh_tsconfig
// lists: rollup is named by its own exports["."].types, the other two only by
// their package directory.
const REQUIRED = ['rollup', 'vite', 'postcss'];

// The generated data file itself, for the one field no fixture can stand in
// for: `dir`, the directory an entry's declarations install under when that is
// not the key it answers. Reading the real pair is the point -- the fixtures in
// worker_map_test.mjs and fragment_map_test.mjs hand-write theirs, so an aspect
// that stopped writing `dir` would leave both green while every @types/* alias
// in a real editor looked under `.bazel/npm/<name it types>`, which nothing
// installs.
const hookData = JSON.parse(
  fs.readFileSync(path.join(workspaceRoot, '.bazel/tsserver-hook-data.json'), 'utf8')
);
// A subpath entry also answers a key its install directory does not spell, and
// its directory legitimately keeps a key of its own: `@vitest/mocker/node` and
// `@vitest/mocker` are both specifiers. The exclusivity below is the @types/*
// alias rule, so only a key that is not a subpath of `dir` is one of those.
const ALIASED = (hookData.npmPackages || []).filter(
  (pkg) => pkg.dir && pkg.dir !== pkg.name && !pkg.name.startsWith(`${pkg.dir}/`)
);

let failures = 0;
const pass = (msg) => process.stdout.write(`PASS: ${msg}\n`);
const fail = (msg) => {
  process.stderr.write(`FAIL: ${msg}\n`);
  failures += 1;
};

const worker = new Worker(workerPath, { workerData: { workspaceRoot } });

const timeout = setTimeout(() => {
  worker.terminate();
  process.stderr.write('FAIL: worker sent no resolution map within 60s\n');
  process.exit(1);
}, 60000);

worker.on('error', (err) => {
  clearTimeout(timeout);
  process.stderr.write(`FAIL: worker error: ${err.stack || err.message}\n`);
  process.exit(1);
});

worker.once('message', (msg) => {
  clearTimeout(timeout);
  if (msg.type !== 'resolution-map') {
    process.stderr.write(`FAIL: unexpected message type ${msg.type}\n`);
    process.exit(1);
  }
  const map = msg.data;
  const modules = Object.keys(map).filter((k) => !k.startsWith('__alias__'));
  process.stdout.write(`INFO: ${modules.length} module entries\n`);

  for (const pkg of REQUIRED) {
    if (!(pkg in map)) {
      fail(`'${pkg}' is not in the resolution map`);
    } else if (!/\.d\.(ts|mts|cts)$/.test(map[pkg])) {
      fail(`'${pkg}' does not resolve to a declaration file: ${map[pkg]}`);
    } else {
      pass(`'${pkg}' -> ${path.relative(workspaceRoot, map[pkg])}`);
    }
  }

  if (!ALIASED.length) {
    fail(
      'the generated data names no package under a name other than its own, so ' +
        'nothing here exercises `dir` -- has the @types/* alias stopped being emitted?'
    );
  }
  for (const pkg of ALIASED) {
    const under = path.join(workspaceRoot, hookData.npmDir, pkg.dir);
    if (!(pkg.name in map)) {
      fail(`'${pkg.name}' is not in the map: ${pkg.dir} was looked for under the wrong name`);
    } else if (!map[pkg.name].startsWith(under + path.sep)) {
      fail(`'${pkg.name}' resolves to ${map[pkg.name]}, which is not inside ${pkg.dir}`);
    } else {
      pass(`'${pkg.name}' -> ${path.relative(workspaceRoot, map[pkg.name])}`);
    }
    if (pkg.dir in map) fail(`'${pkg.dir}' took a key of its own, which no import writes`);
  }

  // Every builder in the worker checks the file before recording it, so a
  // missing path is a bug in the map rather than an unbuilt package.
  for (const key of modules) {
    if (!fs.existsSync(map[key])) fail(`${key} -> ${map[key]} is not on disk`);
  }
  if (!failures) pass('every resolution entry points at a file on disk');

  worker.terminate().then(() => {
    if (failures > 0) {
      process.stderr.write(`\n${failures} FAILED\n`);
      process.exit(1);
    }
    process.stdout.write('\nALL PASSED\n');
    process.exit(0);
  });
});
