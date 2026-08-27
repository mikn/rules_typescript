/**
 * watcher_test.mjs — the HMR watcher of the bundle a dev server actually loads.
 *
 *   node watcher_test.mjs <vite_plugin_bazel.mjs>
 *
 * Both watch paths are exercised: Vite's own `server.watcher` (what the plugin
 * injects) and the node:fs.watch fallback, against real files on disk. The
 * fallback case is what caught `import('chokidar')` — chokidar is bundled inside
 * Vite's dist, so it never resolved and every rebuild was silently ignored.
 *
 * The plugin-level tests model one more thing about Vite that a plugin can get
 * silently wrong: a function returned from `configureServer` is a POST HOOK,
 * which Vite CALLS as soon as its internal middlewares are installed. So the
 * fake server calls it, the way Vite does.
 */

import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { pathToFileURL } from 'node:url';

const bundlePath = process.argv[2];
if (!bundlePath || !fs.existsSync(bundlePath)) {
  process.stderr.write(`FATAL: bundle not found: ${bundlePath}\n`);
  process.exit(1);
}

const { BazelWatcher, bazelPlugin, bazelPathToModuleId } = await import(
  pathToFileURL(bundlePath).href
);

const tmpRoot = process.env.TEST_TMPDIR || os.tmpdir();
let binCounter = 0;
const newBazelBin = () => {
  const dir = path.join(tmpRoot, `bazel-bin-${binCounter++}`);
  fs.mkdirSync(dir, { recursive: true });
  return dir;
};

const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));

async function waitUntil(what, predicate, timeoutMs = 10000) {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    if (predicate()) return;
    await sleep(10);
  }
  throw new Error(`timed out after ${timeoutMs} ms waiting for ${what}`);
}

/** A stand-in for Vite's `server.watcher`, with the surface the plugin uses. */
function fakeViteWatcher() {
  const listeners = new Map();
  return {
    added: [],
    unwatched: [],
    closed: false,
    add(p) {
      this.added.push(p);
      return this;
    },
    unwatch(p) {
      this.unwatched.push(p);
      return this;
    },
    on(event, listener) {
      if (!listeners.has(event)) listeners.set(event, []);
      listeners.get(event).push(listener);
      return this;
    },
    off(event, listener) {
      const fns = listeners.get(event) ?? [];
      const i = fns.indexOf(listener);
      if (i !== -1) fns.splice(i, 1);
      return this;
    },
    close() {
      this.closed = true;
      return Promise.resolve();
    },
    listenerCount(event) {
      return (listeners.get(event) ?? []).length;
    },
    emit(event, filePath) {
      for (const fn of [...(listeners.get(event) ?? [])]) fn(filePath);
    },
  };
}

function fakeDevServer(watcher) {
  const warnings = [];
  const infos = [];
  const sent = [];
  const invalidated = [];
  const logger = {
    info(message) {
      infos.push(message);
    },
    warn(message) {
      warnings.push(message);
    },
    error(message) {
      warnings.push(message);
    },
  };
  return {
    warnings,
    infos,
    sent,
    invalidated,
    restarts: 0,
    logger,
    watcher,
    restart() {
      this.restarts++;
      return Promise.resolve();
    },
    config: { root: tmpRoot, logger },
    moduleGraph: {
      getModulesByFile: (file) => new Set([{ file }]),
      invalidateModule: (mod) => invalidated.push(mod.file),
      invalidateAll: () => invalidated.push('*'),
    },
    ws: { send: (payload) => sent.push(payload) },
  };
}

/**
 * Runs `configureServer` the way Vite does: a function it returns is a post hook
 * that Vite invokes once the internal middlewares are in place, not a teardown
 * handle it stores for shutdown. A plugin that returns its teardown from here
 * detaches its own watchers before the first request.
 */
async function installPlugin(plugin, server) {
  const post = await plugin.configureServer(server);
  if (typeof post === 'function') post();
  return post;
}

const tests = [];
const test = (name, fn) => tests.push([name, fn]);

class Skipped extends Error {}
const skip = (why) => {
  throw new Skipped(why);
};

// fs.watch reaches the filesystem through FSEvents on macOS, and a sandboxed CI
// runner can leave a recursive watch that never fires at all -- which makes the
// two fallback tests below time out and the third, asserting that nothing
// arrives after stop(), vacuously green. Probing once tells the three of them
// apart from a real regression.
let fsWatchDelivers = null;
async function fsWatchDeliversEvents() {
  if (fsWatchDelivers !== null) return fsWatchDelivers;
  const bin = newBazelBin();
  let fired = false;
  const probe = fs.watch(bin, { recursive: true, persistent: true }, () => {
    fired = true;
  });
  try {
    fs.writeFileSync(path.join(bin, 'probe.js'), '1');
    const deadline = Date.now() + 5000;
    while (!fired && Date.now() < deadline) await sleep(20);
  } finally {
    probe.close();
  }
  fsWatchDelivers = fired;
  return fired;
}

const NO_FS_EVENTS = 'recursive fs.watch delivered no events in this environment';

// ── The fs.watch fallback: real files, real events ───────────────────────────

test('fs.watch fallback reports a newly written .js file', async () => {
  if (!(await fsWatchDeliversEvents())) skip(NO_FS_EVENTS);
  const bin = newBazelBin();
  const batches = [];
  const watcher = new BazelWatcher({
    bazelBin: bin,
    debounceMs: 20,
    onRebuild: (changed) => batches.push([...changed]),
  });

  await watcher.start();
  try {
    fs.writeFileSync(path.join(bin, 'app.js'), 'export const a = 1;\n');
    await waitUntil('the first rebuild batch', () => batches.length > 0);
    assert.deepEqual(batches[0], [path.join(bin, 'app.js')]);
  } finally {
    await watcher.stop();
  }
});

test('fs.watch fallback coalesces a rebuild burst and drops non-.js outputs', async () => {
  if (!(await fsWatchDeliversEvents())) skip(NO_FS_EVENTS);
  const bin = newBazelBin();
  const batches = [];
  const watcher = new BazelWatcher({
    bazelBin: bin,
    debounceMs: 200,
    onRebuild: (changed) => batches.push([...changed].sort()),
  });

  await watcher.start();
  try {
    fs.mkdirSync(path.join(bin, 'app'), { recursive: true });
    fs.writeFileSync(path.join(bin, 'a.js'), '1');
    fs.writeFileSync(path.join(bin, 'app', 'b.js'), '2');
    fs.writeFileSync(path.join(bin, 'a.js.map'), '{}');
    fs.writeFileSync(path.join(bin, 'styles.css'), 'a{}');

    await waitUntil('the coalesced batch', () => batches.length > 0);
    await sleep(400);

    assert.equal(batches.length, 1, `expected one batch, got ${batches.length}`);
    assert.deepEqual(batches[0], [
      path.join(bin, 'a.js'),
      path.join(bin, 'app', 'b.js'),
    ]);
  } finally {
    await watcher.stop();
  }
});

test('fs.watch fallback stops reporting after stop()', async () => {
  if (!(await fsWatchDeliversEvents())) skip(NO_FS_EVENTS);
  const bin = newBazelBin();
  const batches = [];
  const watcher = new BazelWatcher({
    bazelBin: bin,
    debounceMs: 20,
    onRebuild: (changed) => batches.push([...changed]),
  });

  await watcher.start();
  await watcher.stop();
  fs.writeFileSync(path.join(bin, 'late.js'), '1');
  await sleep(300);
  assert.deepEqual(batches, []);
});

// ── Vite's own watcher: the path the plugin takes ────────────────────────────

test("an injected source is added to, filtered, and left open by stop()", async () => {
  const bin = newBazelBin();
  const source = fakeViteWatcher();
  const batches = [];
  const watcher = new BazelWatcher({
    bazelBin: bin,
    debounceMs: 20,
    source,
    onRebuild: (changed) => batches.push([...changed]),
  });

  await watcher.start();
  assert.deepEqual(source.added, [bin], 'bazel-bin must be added to the watcher');

  source.emit('change', path.join(bin, 'app.js'));
  source.emit('add', path.join(bin, 'nested', 'x.js'));
  source.emit('change', path.join(bin, 'styles.css'));
  source.emit('change', path.join(tmpRoot, 'outside', 'other.js'));

  await waitUntil('the rebuild batch', () => batches.length > 0);
  assert.deepEqual(batches[0].sort(), [
    path.join(bin, 'app.js'),
    path.join(bin, 'nested', 'x.js'),
  ]);

  await watcher.stop();
  assert.equal(source.listenerCount('change'), 0, 'listeners must be detached');
  assert.deepEqual(source.unwatched, [bin]);
  assert.equal(source.closed, false, "Vite's watcher must not be closed");
});

// ── The failure is never silent ──────────────────────────────────────────────

test('start() rejects with an actionable message when bazel-bin is missing', async () => {
  const watcher = new BazelWatcher({
    bazelBin: path.join(tmpRoot, 'no-such-bazel-bin'),
    onRebuild: () => {},
  });
  await assert.rejects(() => watcher.start(), /bazel-bin does not exist/);
});

// ── The plugin end to end ────────────────────────────────────────────────────

test('configureServer wires the watcher to server.watcher and sends HMR updates', async () => {
  const bin = newBazelBin();
  const source = fakeViteWatcher();
  const server = fakeDevServer(source);
  const plugin = bazelPlugin({ bazelBin: bin, hmrDebounceMs: 20 });

  plugin.configResolved(server.config);
  const post = await installPlugin(plugin, server);

  assert.equal(post, undefined, 'configureServer must return nothing: Vite calls what it returns');
  assert.deepEqual(source.added, [bin], 'the plugin must ride on server.watcher');

  const changed = path.join(bin, 'app', 'page.js');
  source.emit('change', changed);
  await waitUntil('an HMR update on the wire', () => server.sent.length > 0);

  assert.equal(server.sent[0].type, 'update');
  assert.deepEqual(
    server.sent[0].updates.map((u) => u.path),
    [bazelPathToModuleId(changed, bin)],
  );
  assert.deepEqual(server.warnings, []);

  plugin.closeBundle();
  assert.equal(source.listenerCount('change'), 0);
});

test('a watcher that cannot start warns, and throws when hmr is required', async () => {
  const missing = path.join(tmpRoot, 'never-built');
  const server = fakeDevServer(fakeViteWatcher());
  const plugin = bazelPlugin({ bazelBin: missing });

  plugin.configResolved(server.config);
  await installPlugin(plugin, server);

  assert.equal(server.warnings.length, 1, 'the failure must be reported');
  assert.match(server.warnings[0], /no HMR/);
  assert.match(server.warnings[0], /bazel-bin does not exist/);

  const strict = bazelPlugin({ bazelBin: missing, hmr: true });
  const strictServer = fakeDevServer(fakeViteWatcher());
  strict.configResolved(strictServer.config);
  await assert.rejects(() => strict.configureServer(strictServer), /no HMR/);
});

// ── Restart or keep: what a rebuild means ────────────────────────────────────

test('a changed config input restarts the server, a codegen rebuild does not', async () => {
  const bin = newBazelBin();
  const configPath = path.join(bin, 'app', 'dev', 'vite.config.mjs');
  fs.mkdirSync(path.dirname(configPath), { recursive: true });
  fs.writeFileSync(configPath, '// v1\n');
  const codegen = path.join(bin, 'app', 'routes.gen.js');
  fs.writeFileSync(codegen, 'export const routes = [1];\n');

  const source = fakeViteWatcher();
  const server = fakeDevServer(source);
  const plugin = bazelPlugin({
    bazelBin: bin,
    hmrDebounceMs: 20,
    configInputs: [
      {
        label: 'the generated vite config',
        path: configPath,
        digest: 'content',
        remedy: 'restart',
      },
    ],
  });

  plugin.configResolved(server.config);
  await installPlugin(plugin, server);
  assert.ok(source.added.includes(configPath), 'the config input must be watched');

  // A rebuild that only rewrote generated code: the running server is still
  // configured for the graph it has.
  fs.writeFileSync(codegen, 'export const routes = [1, 2];\n');
  source.emit('change', codegen);
  await waitUntil('the HMR update for the codegen rebuild', () => server.sent.length > 0);
  await sleep(200);
  assert.equal(server.restarts, 0, 'a codegen-only rebuild must not restart Vite');

  // A rebuild that rewrote the config: the graph it describes is gone.
  fs.writeFileSync(configPath, '// v2 — new aliases\n');
  source.emit('change', configPath);
  await waitUntil('the restart', () => server.restarts > 0);
  assert.match(server.infos.join('\n'), /restarting: the generated vite config/);

  plugin.closeBundle();
});

test('a config input a restart cannot fix says so', async () => {
  const bin = newBazelBin();
  const npmTree = path.join(bin, 'node_modules_stamp');
  fs.writeFileSync(npmTree, '{"version":"6.0.0"}\n');

  const source = fakeViteWatcher();
  const server = fakeDevServer(source);
  const plugin = bazelPlugin({
    bazelBin: bin,
    hmrDebounceMs: 20,
    configInputs: [
      { label: 'vite in the Bazel npm tree', path: npmTree, digest: 'content', remedy: 'manual' },
    ],
  });

  plugin.configResolved(server.config);
  await installPlugin(plugin, server);

  fs.writeFileSync(npmTree, '{"version":"7.0.0"}\n');
  source.emit('change', npmTree);
  await waitUntil('the restart', () => server.restarts > 0);
  assert.match(server.warnings.join('\n'), /re-run `bazel run` on this target/);

  plugin.closeBundle();
});

test('hmr: false starts no watcher at all', async () => {
  const bin = newBazelBin();
  const source = fakeViteWatcher();
  const server = fakeDevServer(source);
  const plugin = bazelPlugin({ bazelBin: bin, hmr: false });

  plugin.configResolved(server.config);
  const post = await installPlugin(plugin, server);

  assert.equal(post, undefined);
  assert.deepEqual(source.added, []);
  assert.deepEqual(server.warnings, []);
});

let failed = 0;
let skipped = 0;
for (const [name, fn] of tests) {
  try {
    await fn();
    process.stdout.write(`PASS: ${name}\n`);
  } catch (err) {
    if (err instanceof Skipped) {
      skipped++;
      process.stdout.write(`SKIP: ${name} -- ${err.message}\n`);
      continue;
    }
    failed++;
    process.stdout.write(`FAIL: ${name}\n${err && err.stack ? err.stack : err}\n`);
  }
}

const passed = tests.length - failed - skipped;
process.stdout.write(`\n${passed}/${tests.length} passed${skipped ? `, ${skipped} skipped` : ''}\n`);
process.exit(failed === 0 ? 0 : 1);
