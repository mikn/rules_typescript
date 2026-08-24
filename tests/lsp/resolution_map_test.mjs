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
