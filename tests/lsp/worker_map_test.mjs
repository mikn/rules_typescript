/**
 * worker_map_test.mjs — the tsserver-hook worker's resolution map, hermetically.
 *
 *   node worker_map_test.mjs <tsserver-hook-worker.js>
 *
 * The worker reads what `bazel run //:refresh_tsconfig` wrote from the build
 * graph -- .bazel/tsserver-hook-data.json -- so a fixture workspace is the whole
 * of its input, and there is no `bazel` left to stub out.
 *
 * Both halves of the map are checked here, including the parts that are left
 * OUT: a ts_compile package with no entry point, a nested workspace's
 * directives, and a "~" alias prefix that the worker's character screen rejects
 * even though gazelle accepts it.
 */

import { Worker } from 'node:worker_threads';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';

const workerPath = process.argv[2];
if (!workerPath || !fs.existsSync(workerPath)) {
  process.stderr.write(`FATAL: worker not found: ${workerPath}\n`);
  process.exit(1);
}

const root = fs.mkdtempSync(path.join(process.env.TEST_TMPDIR || os.tmpdir(), 'wsroot-'));

const write = (rel, contents) => {
  const p = path.join(root, rel);
  fs.mkdirSync(path.dirname(p), { recursive: true });
  fs.writeFileSync(p, contents);
  return p;
};

// ── The fixture workspace ────────────────────────────────────────────────────

write('MODULE.bazel', 'module(name = "fixture")\n');
write(
  'BUILD.bazel',
  [
    '# gazelle:ts_path_alias @/ src/',
    // The worker screens alias prefixes against [A-Za-z0-9@/_.*-], so a "~"
    // prefix -- which gazelle itself accepts and writes into tsconfig paths --
    // is dropped here.
    '# gazelle:ts_path_alias ~lib/ packages/lib/src/',
    '',
  ].join('\n')
);

// An internal package whose entry point exists only in source.
const libIndex = write('src/lib/index.ts', 'export const a = 1;\n');
// An internal package built into bazel-bin: the .d.ts there must win over the
// .ts source, because that is what the editor should be type-checking against.
write('src/app/index.ts', 'export const b = 2;\n');
const appDts = write('bazel-bin/src/app/index.d.ts', 'export declare const b: number;\n');
// An internal package with no index file at all: nothing to resolve to.
write('src/empty/helpers.ts', 'export const c = 3;\n');

// A nested workspace. Its directives belong to that workspace, so the walk must
// stop at the boundary rather than adopting them.
write('vendor/child/MODULE.bazel', 'module(name = "child")\n');
write('vendor/child/BUILD.bazel', '# gazelle:ts_path_alias @child/ vendor/child/src/\n');

// ── The graph data ───────────────────────────────────────────────────────────

write(
  '.bazel/tsserver-hook-data.json',
  JSON.stringify({ packages: ['src/lib', 'src/app', 'src/empty'] })
);

// ── Run the worker and check the map it sends ───────────────────────────────

let failures = 0;
const pass = (name) => process.stdout.write(`PASS: ${name}\n`);
const fail = (name, detail) => {
  process.stderr.write(`FAIL: ${name}${detail ? ': ' + detail : ''}\n`);
  failures += 1;
};

function expectEntry(map, key, want) {
  if (!(key in map)) {
    fail(`${key} is in the map`, `keys: ${JSON.stringify(Object.keys(map).sort())}`);
    return;
  }
  if (map[key] !== want) {
    fail(`${key} resolves correctly`, `got ${map[key]}, want ${want}`);
    return;
  }
  if (!fs.existsSync(map[key])) {
    fail(`${key} points at a real path`, map[key]);
    return;
  }
  pass(`${key} -> ${map[key]}`);
}

function expectAbsent(map, key, why) {
  if (key in map) {
    fail(`${key} must NOT be in the map (${why})`, `got ${map[key]}`);
    return;
  }
  pass(`${key} is absent (${why})`);
}

const worker = new Worker(workerPath, { workerData: { workspaceRoot: root } });

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
  process.stdout.write(`INFO: map = ${JSON.stringify(map, null, 2)}\n`);

  // Internal ts_compile packages, keyed by package path.
  expectEntry(map, 'src/lib', libIndex);
  expectEntry(map, 'src/app', appDts);
  expectAbsent(map, 'src/empty', 'no index.ts/index.d.ts to resolve to');

  // Path aliases from BUILD directives.
  expectEntry(map, '__alias__@/', path.join(root, 'src'));
  expectAbsent(map, '__alias__@child/', 'the walk stops at a nested workspace boundary');
  expectAbsent(map, '__alias__~lib/', 'a "~" prefix fails the worker\'s character screen');

  worker.terminate().then(() => {
    if (failures > 0) {
      process.stderr.write(`\n${failures} FAILED\n`);
      process.exit(1);
    }
    process.stdout.write('\nALL PASSED\n');
    process.exit(0);
  });
});
