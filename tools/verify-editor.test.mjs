import { FSWatcher } from "chokidar";
import assert from "node:assert/strict";
import childProcess, { spawn, spawnSync } from "node:child_process";
import fs from "node:fs";
import fsPromises from "node:fs/promises";
import {
  mkdtempSync,
  mkdirSync,
  readFileSync,
  statSync,
  rmSync,
  symlinkSync,
  writeFileSync,
  unlinkSync,
  renameSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { syncBuiltinESMExports } from "node:module";
import { fileURLToPath, pathToFileURL } from "node:url";
import { EventEmitter, once } from "node:events";
import { createServer } from "node:net";
import test from "node:test";
import {
  checkCompilerErrors,
  connect,
  languageId,
  watchRegistration,
  watchScope,
  withNativeSession,
} from "./verify-editor.mjs";

function fixture(onRequest, onFailure = () => {}) {
  const child = new EventEmitter();
  child.stdout = new EventEmitter();
  child.stdin = new EventEmitter();
  const writes = [];
  child.stdin.write = (value) => writes.push(value);
  child.kill = () => {};
  return { child, writes, client: connect(child, onFailure, onRequest) };
}

function frame(message) {
  const body = Buffer.from(JSON.stringify({ jsonrpc: "2.0", ...message }));
  return Buffer.concat([Buffer.from(`Content-Length: ${body.length}\r\n\r\n`), body]);
}

test("split UTF-8 and adjacent frames retain response identity", async () => {
  const { child, client } = fixture();
  const first = client.request("first", {});
  const second = client.request("second", {});
  const bytes = Buffer.concat([frame({ id: 2, result: "é😀" }), frame({ id: 1, result: [] })]);
  for (const byte of bytes) child.stdout.emit("data", Buffer.from([byte]));
  assert.deepEqual(await first, []);
  assert.equal(await second, "é😀");
});

test("server registration is acknowledged without consuming a pending response", async () => {
  let installed;
  const installation = new Promise((resolve) => {
    installed = resolve;
  });
  const { child, client, writes } = fixture(async () => {
    await installation;
    return null;
  });
  const result = client.request("initialize", {});
  child.stdout.emit(
    "data",
    frame({ id: "registration", method: "client/registerCapability", params: {} }),
  );
  assert.equal(writes.length, 1);
  installed();
  await new Promise((resolve) => setImmediate(resolve));
  assert.match(writes[1].toString(), /"id":"registration","result":null/);
  child.stdout.emit("data", frame({ id: 1, result: {} }));
  assert.deepEqual(await result, {});
});

test("server errors and process exit reject requests instead of leaving them pending", async () => {
  const { child, client } = fixture();
  const failed = client.request("definition", {});
  child.stdout.emit("data", frame({ id: 1, error: { code: -32603, message: "failure" } }));
  await assert.rejects(failed, /failure/);
  const pending = client.request("diagnostic", {});
  child.emit("exit", 1, null);
  await assert.rejects(pending, /exited 1/);
  await assert.rejects(client.request("after-exit", {}), /exited 1/);
});

test("malformed framing rejects pending and later requests", async () => {
  const { child, client } = fixture();
  const pending = client.request("diagnostic", {});
  child.stdout.emit("data", Buffer.from("Content-Length: x\r\n\r\n"));
  await assert.rejects(pending, /Content-Length/);
  await assert.rejects(client.request("after-failure", {}), /Content-Length/);
});

test("TSX and JSX documents retain their React language identity", () => {
  for (const [file, expected] of [
    ["view.tsx", "typescriptreact"],
    ["view.jsx", "javascriptreact"],
    ["module.ts", "typescript"],
    ["module.mts", "typescript"],
    ["module.cts", "typescript"],
    ["module.js", "javascript"],
    ["module.mjs", "javascript"],
    ["module.cjs", "javascript"],
  ])
    assert.equal(languageId(file), expected);
  assert.throws(() => languageId("unknown.json"), /unsupported probe/);
});

test("symlinked verifier cannot skip CLI validation and exit successfully", () => {
  const directory = mkdtempSync(join(tmpdir(), "verify-editor-entry-"));
  try {
    const direct = fileURLToPath(new URL("./verify-editor-cli.mjs", import.meta.url));
    const link = join(directory, "verify-editor.mjs");
    symlinkSync(direct, link);
    for (const entry of [direct, link]) {
      const result = spawnSync(process.execPath, [entry], { encoding: "utf8" });
      assert.ifError(result.error);
      assert.equal(result.status, 1, entry);
      assert.equal(result.stdout, "", entry);
      assert.match(
        result.stderr,
        /usage: node verify-editor\.mjs <tsgo-executable> <probes\.json>/,
        entry,
      );
    }
  } finally {
    rmSync(directory, { recursive: true, force: true });
  }
});

test("compiler sanity rejects unexpected errors without comparing editor hints", () => {
  checkCompilerErrors({ status: 0, stdout: "", stderr: "" }, false);
  checkCompilerErrors(
    { status: 1, stdout: "file.ts(1,1): error TS2339: missing member", stderr: "" },
    true,
  );
  assert.throws(() => checkCompilerErrors({ status: 0, stdout: "", stderr: "" }, true));
  assert.throws(() =>
    checkCompilerErrors({ status: 1, stdout: "error TS2339: missing member", stderr: "" }, false),
  );
});

test("request cancellation preserves session and consumes late canceled response", async () => {
  const { child, client, writes } = fixture();
  const controller = new AbortController();
  const pending = client.request("completion", {}, controller.signal);
  controller.abort(new Error("tool canceled"));
  await assert.rejects(pending, /tool canceled/);
  assert.match(writes[1].toString(), /"method":"\$\/cancelRequest","params":\{"id":1\}/);
  child.stdout.emit("data", frame({ id: 1, error: { code: -32800, message: "cancelled" } }));
  const next = client.request("hover", {});
  child.stdout.emit("data", frame({ id: 2, result: "alive" }));
  assert.equal(await next, "alive");
});

function observedRegistration(directory, observe = () => {}) {
  let pending;
  let failure;
  const watcher = watchRegistration(
    {
      method: "workspace/didChangeWatchedFiles",
      registerOptions: { watchers: [{ globPattern: directory + "/**/*" }] },
    },
    (method, params) => {
      observe("notification", { method, ...params });
      assert.equal(method, "workspace/didChangeWatchedFiles");
      for (const event of params.changes) {
        if (event.uri === pending?.uri && event.type === pending.type) {
          const { resolve } = pending;
          pending = undefined;
          resolve(event);
        }
      }
    },
    (error) => {
      observe("error", { message: error.message, code: error.code, path: error.path });
      failure ??= error;
      pending?.reject(failure);
      pending = undefined;
    },
  );
  return {
    ...watcher,
    next(file, type) {
      if (failure) return Promise.reject(failure);
      assert.equal(pending, undefined);
      return new Promise((resolve, reject) => {
        pending = { uri: pathToFileURL(file).href, type, resolve, reject };
      });
    },
  };
}

test("compiler registered directory cannot miss creation after readiness or deletion before unregister (#233)", async (t) => {
  const directory = mkdtempSync(join(tmpdir(), "native-watch-"));
  const file = join(directory, "generated.ts");
  const observations = [];
  let diagnosticsLive = false;
  const flush = () => {
    for (const observation of observations.splice(0)) {
      process.stderr.write(`watch boundary (#233): ${JSON.stringify(observation)}\n`);
    }
  };
  const observe = (boundary, facts) => {
    observations.push({ boundary, ...facts });
    if (diagnosticsLive) flush();
  };
  const nativeWatch = fs.watch;
  t.mock.method(fs, "watch", (path, options, listener) => {
    const handle = nativeWatch(path, options, (event, filename) => {
      observe("native event", { path, recursive: options.recursive, event, filename });
      return listener(event, filename);
    });
    observe("native acquisition", { path, recursive: options.recursive });
    return handle;
  });
  syncBuiltinESMExports();
  const add = FSWatcher.prototype.add;
  t.mock.method(FSWatcher.prototype, "add", function (...args) {
    if (args[0] === directory) {
      this.on("raw", (event, path, details) => observe("chokidar raw", { event, path, details }));
      this.on("all", (event, path) => observe("chokidar all", { event, path }));
      this.once("ready", () =>
        observe("chokidar ready", { closed: this.closed, watched: this.getWatched() }),
      );
    }
    return add.apply(this, args);
  });
  const watcher = observedRegistration(directory, observe);
  const stage = async (name, operation) => {
    process.stderr.write(`watch registration (#233): waiting for ${name}\n`);
    await operation();
    process.stderr.write(`watch registration (#233): completed ${name}\n`);
  };
  try {
    await stage("ready", () => watcher.ready);
    await stage("generated.ts creation", async () => {
      const created = watcher.next(file, 1);
      writeFileSync(file, "export const first = 1;\n");
      diagnosticsLive = true;
      observe("creation written", { uri: pathToFileURL(file).href });
      await created;
    });
    await stage("generated.ts deletion", async () => {
      const deleted = watcher.next(file, 3);
      unlinkSync(file);
      await deleted;
    });
    await stage("close", () => watcher.close());
    await watcher.close();
    for (const pattern of [
      "guessed/*.ts",
      directory + "/*.json",
      directory + "/{a,b}",
      directory + "/[ab].json",
      directory + "/?.json",
    ]) {
      assert.throws(
        () =>
          watchRegistration(
            {
              method: "workspace/didChangeWatchedFiles",
              registerOptions: { watchers: [{ globPattern: pattern }] },
            },
            () => {},
            () => {},
          ),
        /unsupported native watch pattern/,
      );
    }
  } finally {
    await watcher.close();
    flush();
    t.mock.restoreAll();
    syncBuiltinESMExports();
    rmSync(directory, { recursive: true, force: true });
  }
});

test("watcher error after readiness rejects pending and later file observations (#233)", async (t) => {
  const directory = mkdtempSync(join(tmpdir(), "native-watch-observation-error-"));
  const nativeWatch = fs.watch;
  let nativeHandle;
  t.mock.method(fs, "watch", (path, ...args) => {
    const handle = nativeWatch(path, ...args);
    if (path === directory) nativeHandle = handle;
    return handle;
  });
  syncBuiltinESMExports();
  const watcher = observedRegistration(directory);
  try {
    await watcher.ready;
    assert.ok(nativeHandle);
    const file = join(directory, "generated.ts");
    const failed = watcher.next(file, 1);
    const error = Object.assign(new Error("native watch failed after readiness"), {
      code: "EIO",
      path: directory,
    });
    const rejected = assert.rejects(failed, (actual) => actual === error);
    nativeHandle.emit("error", error);
    await rejected;
    await assert.rejects(watcher.next(file, 3), (actual) => actual === error);
  } finally {
    await watcher.close();
    t.mock.restoreAll();
    syncBuiltinESMExports();
    rmSync(directory, { recursive: true, force: true });
  }
});

test("watch notification transport failure reaches cleanup owner", async () => {
  const directory = mkdtempSync(join(tmpdir(), "native-watch-failure-"));
  let rejected;
  const failure = new Promise((resolve) => {
    rejected = resolve;
  });
  const watcher = watchRegistration(
    {
      method: "workspace/didChangeWatchedFiles",
      registerOptions: {
        watchers: [
          { globPattern: { baseUri: new URL("file://" + directory).href, pattern: "**/*" } },
        ],
      },
    },
    () => {
      throw new Error("transport closed");
    },
    rejected,
  );
  try {
    await watcher.ready;
    writeFileSync(join(directory, "file.ts"), "export {};\n");
    assert.match(String(await failure), /transport closed/);
  } finally {
    await watcher.close();
    rmSync(directory, { recursive: true, force: true });
  }
});

test("change-only compiler watch observes atomic replacement", async () => {
  const directory = mkdtempSync(join(tmpdir(), "native-watch-replace-"));
  const target = join(directory, "value.ts");
  writeFileSync(target, "before");
  let changed;
  const observed = new Promise((resolve) => {
    changed = resolve;
  });
  const watcher = watchRegistration(
    {
      method: "workspace/didChangeWatchedFiles",
      registerOptions: { watchers: [{ globPattern: directory + "/**/*", kind: 2 }] },
    },
    (_, params) => {
      if (params.changes.some((item) => item.uri.endsWith("/value.ts") && item.type === 2))
        changed();
    },
    (error) => {
      throw error;
    },
  );
  try {
    await watcher.ready;
    writeFileSync(join(directory, "temporary"), "after");
    renameSync(join(directory, "temporary"), target);
    await observed;
  } finally {
    await watcher.close();
    rmSync(directory, { recursive: true, force: true });
  }
});

test("native recursive registrations cannot subscribe to a filesystem root", () => {
  for (const pattern of ["/**/*", { baseUri: "file:///", pattern: "**/*" }])
    assert.throws(() => watchScope(pattern), /filesystem root/);
  assert.deepEqual(watchScope("/package.json"), {
    requested: "/",
    fileTarget: "/package.json",
  });
  const directory = mkdtempSync(join(tmpdir(), "native-watch-root-"));
  try {
    const alias = join(directory, "root");
    symlinkSync("/", alias);
    assert.throws(() => watchScope(alias + "/**/*"), /filesystem root/);
  } finally {
    rmSync(directory, { recursive: true, force: true });
  }
});

test(
  "macOS UNKNOWN socket watches cannot abort recursive registration or file replacement (#233)",
  { skip: process.platform === "win32" },
  async (t) => {
    const directory = fs.realpathSync(mkdtempSync(join(tmpdir(), "watch-")));
    const target = join(directory, "generated.ts");
    const sibling = join(directory, "sibling.ts");
    const socket = createServer();
    socket.listen(join(directory, "a"));
    await once(socket, "listening");
    renameSync(join(directory, "a"), target);
    writeFileSync(sibling, "before");
    let deferParent = false;
    const deferred = [];
    let fileListener;
    const nativeWatch = fs.watch;
    const socketAttempts = [];
    t.mock.method(fs, "watch", (path, options, listener) => {
      if (statSync(path).isSocket()) {
        socketAttempts.push(path);
        throw Object.assign(new Error("macOS cannot watch Unix sockets"), {
          code: "UNKNOWN",
          path,
        });
      }
      if (path === target) fileListener = listener;
      return nativeWatch(path, options, (...args) => {
        if (path === directory && deferParent) deferred.push(() => listener(...args));
        else listener(...args);
      });
    });
    syncBuiltinESMExports();
    let pending;
    const observed = (path, type) =>
      new Promise((resolve, reject) => {
        pending = { path, type, resolve, reject };
      });
    const failures = [];
    const watcher = watchRegistration(
      {
        method: "workspace/didChangeWatchedFiles",
        registerOptions: { watchers: [{ globPattern: directory + "/**/*" }] },
      },
      (_, params) => {
        if (
          params.changes.some(
            (entry) => fileURLToPath(entry.uri) === pending?.path && entry.type === pending.type,
          )
        )
          pending.resolve();
      },
      (error) => {
        failures.push(error);
        pending?.reject(error);
      },
    );
    try {
      await watcher.ready;
      assert.deepEqual(socketAttempts, []);
      let change = observed(sibling, 2);
      writeFileSync(sibling, "after");
      await change;
      change = observed(target, 1);
      writeFileSync(join(directory, "replacement"), "first generated source");
      renameSync(join(directory, "replacement"), target);
      await change;
      assert.equal(typeof fileListener, "function");
      change = observed(target, 2);
      writeFileSync(target, "edited generated source");
      await change;
      const replacementSocket = createServer();
      try {
        replacementSocket.listen(join(directory, "b"));
        await once(replacementSocket, "listening");
        deferParent = true;
        change = observed(target, 3);
        renameSync(join(directory, "b"), target);
        fileListener("rename", "generated.ts");
        await change;
        assert.deepEqual(socketAttempts, []);
        deferParent = false;
        for (const deliver of deferred.splice(0)) deliver();
        change = observed(target, 1);
        writeFileSync(join(directory, "replacement"), "restored generated source");
        renameSync(join(directory, "replacement"), target);
        await change;
        change = observed(target, 2);
        writeFileSync(target, "edited restored source");
        await change;
      } finally {
        await new Promise((resolve) => replacementSocket.close(resolve));
      }
      assert.deepEqual(failures, []);
      assert.deepEqual(socketAttempts, []);
    } finally {
      await watcher.close();
      await new Promise((resolve) => socket.close(resolve));
      t.mock.restoreAll();
      syncBuiltinESMExports();
      rmSync(directory, { recursive: true, force: true });
    }
  },
);

test(
  "required literal and recursive-root sockets propagate UNKNOWN instead of acknowledging coverage",
  { skip: process.platform === "win32" },
  async (t) => {
    const directory = fs.realpathSync(mkdtempSync(join(tmpdir(), "watch-")));
    const target = join(directory, "required");
    const socket = createServer();
    socket.listen(target);
    await once(socket, "listening");
    const unknown = Object.assign(new Error("required socket cannot be watched"), {
      code: "UNKNOWN",
      path: target,
    });
    const nativeWatch = fs.watch;
    t.mock.method(fs, "watch", (path, ...args) => {
      if (path === target) throw unknown;
      return nativeWatch(path, ...args);
    });
    syncBuiltinESMExports();
    try {
      for (const globPattern of [target, target + "/**/*"]) {
        const failures = [];
        const watcher = watchRegistration(
          {
            method: "workspace/didChangeWatchedFiles",
            registerOptions: { watchers: [{ globPattern }] },
          },
          () => {},
          (error) => failures.push(error),
        );
        try {
          await assert.rejects(watcher.ready, (error) => error === unknown);
          assert.deepEqual(failures, [unknown]);
        } finally {
          await watcher.close();
        }
      }
    } finally {
      await new Promise((resolve) => socket.close(resolve));
      t.mock.restoreAll();
      syncBuiltinESMExports();
      rmSync(directory, { recursive: true, force: true });
    }
  },
);

test("closing a pending registration rejects readiness instead of acknowledging it", async () => {
  const directory = mkdtempSync(join(tmpdir(), "native-watch-pending-"));
  const watcher = watchRegistration(
    {
      method: "workspace/didChangeWatchedFiles",
      registerOptions: { watchers: [{ globPattern: directory + "/**/*" }] },
    },
    () => {},
    (error) => {
      throw error;
    },
  );
  try {
    const rejected = assert.rejects(watcher.ready, /closed before ready/);
    await watcher.close();
    await rejected;
  } finally {
    await watcher.close();
    rmSync(directory, { recursive: true, force: true });
  }
});

test("literal compiler config watch survives replacement without subscribing to siblings", async (t) => {
  const directory = mkdtempSync(join(tmpdir(), "native-watch-config-"));
  const target = join(directory, "tsconfig.build.json");
  const temporary = join(directory, "replacement.json");
  writeFileSync(target, "{}");
  writeFileSync(join(directory, "sibling.ts"), "export {};");
  const subscriptions = [];
  const nativeWatch = fs.watch;
  t.mock.method(fs, "watch", (path, ...args) => {
    subscriptions.push(path);
    return nativeWatch(path, ...args);
  });
  syncBuiltinESMExports();
  let waiting;
  const events = [];
  const observed = (type) =>
    new Promise((resolve, reject) => {
      waiting = { type, resolve, reject };
    });
  const watcher = watchRegistration(
    {
      method: "workspace/didChangeWatchedFiles",
      registerOptions: { watchers: [{ globPattern: target }] },
    },
    (_, params) => {
      events.push(...params.changes);
      if (params.changes.some((entry) => entry.type === waiting?.type)) waiting.resolve();
    },
    (error) => waiting?.reject(error),
  );
  try {
    await watcher.ready;
    for (const contents of ['{ "first": true }', '{ "second": true }']) {
      const changed = observed(2);
      writeFileSync(temporary, contents);
      renameSync(temporary, target);
      await changed;
    }
    const deleted = observed(3);
    unlinkSync(target);
    await deleted;
    const created = observed(1);
    writeFileSync(target, "{}");
    await created;
    assert.ok(events.every((entry) => fileURLToPath(entry.uri) === target));
    assert.ok(subscriptions.length > 0);
    assert.ok(subscriptions.every((path) => path === directory || path === target));
  } finally {
    await watcher.close();
    t.mock.restoreAll();
    syncBuiltinESMExports();
    rmSync(directory, { recursive: true, force: true });
  }
});

test("late replacement stat cannot erase the literal deletion before its pending callback (#233)", async (t) => {
  const directory = mkdtempSync(join(tmpdir(), "native-watch-late-rearm-"));
  const target = join(directory, "tsconfig.json");
  const replacement = join(directory, "replacement.json");
  writeFileSync(target, "original");
  const originalInode = statSync(target).ino;
  const nativeWatch = fs.watch;
  const nativeStat = fsPromises.stat;
  const add = FSWatcher.prototype.add;
  const handles = [];
  const acquisitionErrors = [];
  let fileListener;
  let owner;
  t.mock.method(FSWatcher.prototype, "add", function (...args) {
    if (!owner) {
      owner = this._nodeFsHandler;
      const acquire = owner._watchWithNodeFs;
      t.mock.method(owner, "_watchWithNodeFs", function (path, listener, ...options) {
        if (path === target && !fileListener) fileListener = listener;
        return acquire.call(this, path, listener, ...options);
      });
    }
    return add.apply(this, args);
  });
  t.mock.method(fs, "watch", (path, options) => {
    try {
      const handle = nativeWatch(path, options, () => {});
      const record = { path, closed: false };
      const close = handle.close.bind(handle);
      handle.close = () => {
        record.closed = true;
        return close();
      };
      handles.push(record);
      return handle;
    } catch (error) {
      acquisitionErrors.push({ path, code: error.code });
      throw error;
    }
  });
  let captured;
  const capturedStat = new Promise((resolve) => (captured = resolve));
  let missing;
  const missingStat = new Promise((resolve) => (missing = resolve));
  let releaseReplacement;
  const replacementGate = new Promise((resolve) => (releaseReplacement = resolve));
  let releaseRemoval;
  const removalGate = new Promise((resolve) => (releaseRemoval = resolve));
  let armed = false;
  let checks = 0;
  t.mock.method(fsPromises, "stat", async (path, ...args) => {
    if (!armed || path !== target) return nativeStat(path, ...args);
    if (++checks === 1) {
      const stats = await nativeStat(path, ...args);
      captured(stats);
      await replacementGate;
      return stats;
    }
    if (checks === 2) {
      try {
        return await nativeStat(path, ...args);
      } catch (error) {
        missing(error);
        await removalGate;
        throw error;
      }
    }
    return nativeStat(path, ...args);
  });
  syncBuiltinESMExports();
  const events = [];
  const failures = [];
  const operations = [];
  const watcher = watchRegistration(
    {
      method: "workspace/didChangeWatchedFiles",
      registerOptions: { watchers: [{ globPattern: target }] },
    },
    (_, params) => events.push(...params.changes),
    (error) => failures.push(error),
  );
  try {
    await watcher.ready;
    assert.equal(typeof fileListener, "function");
    writeFileSync(replacement, "replacement");
    renameSync(replacement, target);
    armed = true;
    const replacing = fileListener(target);
    operations.push(replacing);
    assert.notEqual((await capturedStat).ino, originalInode);
    unlinkSync(target);
    const removing = fileListener(target);
    operations.push(removing);
    assert.equal((await missingStat).code, "ENOENT");
    releaseReplacement();
    await replacing;
    assert.deepEqual(acquisitionErrors, [{ path: target, code: "ENOENT" }]);
    assert.equal(
      events.some((event) => event.type === 3),
      false,
    );
    releaseRemoval();
    await removing;
    assert.deepEqual(
      events.filter((event) => event.type === 3),
      [{ uri: pathToFileURL(target).href, type: 3 }],
    );
    assert.deepEqual(failures, []);
  } finally {
    await watcher.close();
    releaseReplacement();
    releaseRemoval();
    await Promise.allSettled(operations);
    t.mock.restoreAll();
    syncBuiltinESMExports();
    rmSync(directory, { recursive: true, force: true });
  }
  assert(handles.every((handle) => handle.closed));
});

test("explicit exit prevents delayed registration responses from writing to a closed server", async () => {
  let release;
  let started;
  const pending = new Promise((resolve) => {
    release = resolve;
  });
  const entered = new Promise((resolve) => {
    started = resolve;
  });
  let handlers = 0;
  const { child, client, writes } = fixture(async () => {
    handlers++;
    started();
    await pending;
    return null;
  });
  const shutdown = client.request("shutdown");
  child.stdout.emit(
    "data",
    Buffer.concat([
      frame({ id: "registration", method: "client/registerCapability", params: {} }),
      frame({ id: 1, result: null }),
    ]),
  );
  await entered;
  await shutdown;
  client.notify("exit");
  release();
  await new Promise((resolve) => setImmediate(resolve));
  child.stdout.emit("data", frame({ id: "late", method: "client/registerCapability", params: {} }));
  await new Promise((resolve) => setImmediate(resolve));
  assert.deepEqual(
    writes.map((bytes) => JSON.parse(bytes.toString().split("\r\n\r\n")[1])),
    [
      { jsonrpc: "2.0", id: 1, method: "shutdown" },
      { jsonrpc: "2.0", method: "exit" },
    ],
  );
  assert.equal(handlers, 1);
});

test("exit before deferred dispatch does not install another server registration", async () => {
  let handlers = 0;
  const { child, client, writes } = fixture(async () => {
    handlers++;
  });
  child.stdout.emit(
    "data",
    frame({ id: "registration", method: "client/registerCapability", params: {} }),
  );
  client.notify("exit");
  await new Promise((resolve) => setImmediate(resolve));
  assert.equal(handlers, 0);
  assert.equal(writes.length, 1);
  await assert.rejects(client.request("after-exit"), /has exited/);
  assert.throws(() => client.notify("after-exit"), /has exited/);
});

test("a rejected handler after exit remains a failure without writing an error reply", async () => {
  let rejectHandler;
  let entered;
  const started = new Promise((resolve) => {
    entered = resolve;
  });
  const pending = new Promise((_, reject) => {
    rejectHandler = reject;
  });
  const failures = [];
  const { child, client, writes } = fixture(
    async () => {
      entered();
      return pending;
    },
    (error) => failures.push(error),
  );
  child.stdout.emit(
    "data",
    frame({ id: "registration", method: "client/registerCapability", params: {} }),
  );
  await started;
  client.notify("exit");
  const failure = new Error("registration failed");
  rejectHandler(failure);
  await new Promise((resolve) => setImmediate(resolve));
  assert.deepEqual(failures, [failure]);
  assert.equal(writes.length, 1);
  await assert.rejects(client.request("after-failure"), /registration failed/);
});

test("generated updates reach both logical aliases and readable cycle paths before disposal", async () => {
  const base = mkdtempSync(join(tmpdir(), "native-watch-aliases-"));
  const root = join(base, "workspace");
  const target = join(base, "generated");
  const dependency = join(base, "dependency");
  for (const path of [root, target, dependency]) mkdirSync(path);
  symlinkSync(target, join(root, "first"));
  symlinkSync(target, join(root, "second"));
  symlinkSync(dependency, join(target, "dependency"));
  symlinkSync(target, join(dependency, "generated"));
  for (const path of [join(target, "value.ts"), join(root, "sibling.ts")])
    writeFileSync(path, "initial");
  const aliases = [
    join(root, "first"),
    join(root, "second"),
    join(root, "first/dependency/generated"),
  ];
  let pending;
  const expect = (paths, type) =>
    new Promise((resolve, reject) => {
      pending = { paths: new Set(paths), type, resolve, reject };
    });
  const watcher = watchRegistration(
    {
      method: "workspace/didChangeWatchedFiles",
      registerOptions: { watchers: [{ globPattern: root + "/**/*" }] },
    },
    (_, params) => {
      for (const event of params.changes) {
        if (event.type === pending?.type) pending.paths.delete(fileURLToPath(event.uri));
      }
      if (pending?.paths.size === 0) pending.resolve();
    },
    (error) => pending?.reject(error),
  );
  try {
    await watcher.ready;
    let observed = expect(
      aliases.map((path) => join(path, "value.ts")).concat(join(root, "sibling.ts")),
      2,
    );
    writeFileSync(join(target, "value.ts"), "intermediate");
    writeFileSync(join(root, "sibling.ts"), "changed");
    await observed;
    observed = expect(
      aliases.map((path) => join(path, "value.ts")),
      2,
    );
    writeFileSync(join(target, "replacement"), "final");
    renameSync(join(target, "replacement"), join(target, "value.ts"));
    await observed;
    for (const alias of aliases)
      assert.equal(readFileSync(join(alias, "value.ts"), "utf8"), "final");
    observed = expect(
      aliases.map((path) => join(path, "added.ts")),
      1,
    );
    writeFileSync(join(target, "added.ts"), "added");
    await observed;
    observed = expect(
      aliases.map((path) => join(path, "added.ts")),
      3,
    );
    unlinkSync(join(target, "added.ts"));
    await observed;
    await watcher.close();
    assert.deepEqual([...pending.paths], []);
  } finally {
    await watcher.close();
    rmSync(base, { recursive: true, force: true });
  }
});

test("late failed file checks cannot reopen a disposed library watcher", async () => {
  const directory = mkdtempSync(join(tmpdir(), "native-watch-late-stat-"));
  const file = join(directory, "value.ts");
  writeFileSync(file, "initial");
  const watcher = new FSWatcher({ ignoreInitial: true });
  let listener;
  watcher._nodeFsHandler._watchWithNodeFs = (_, callback) => {
    listener = callback;
    return () => {};
  };
  let additions = 0;
  watcher.add = () => {
    additions++;
    return watcher;
  };
  try {
    watcher._nodeFsHandler._handleFile(file, statSync(file), true);
    unlinkSync(file);
    const checks = [listener(file), listener(file)];
    await watcher.close();
    await Promise.all(checks);
    assert.equal(additions, 0);
    assert.equal(watcher.closed, true);
    assert.deepEqual(watcher.getWatched(), {});
  } finally {
    await watcher.close();
    rmSync(directory, { recursive: true, force: true });
  }
});

test("closing pending registration returns cancellation instead of acknowledging a closed watch", async () => {
  const directory = mkdtempSync(join(tmpdir(), "native-watch-cancel-"));
  const watcher = watchRegistration(
    {
      method: "workspace/didChangeWatchedFiles",
      registerOptions: { watchers: [{ globPattern: directory + "/**/*" }] },
    },
    () => {},
    (error) => {
      throw error;
    },
  );
  const failures = [];
  const { child, client, writes } = fixture(
    () => watcher.ready,
    (error) => failures.push(error),
  );
  try {
    child.stdout.emit(
      "data",
      frame({ id: "registration", method: "client/registerCapability", params: {} }),
    );
    await Promise.resolve();
    await watcher.close();
    await new Promise((resolve) => setImmediate(resolve));
    const reply = JSON.parse(writes[0].toString().split("\r\n\r\n")[1]);
    assert.equal(reply.error.code, -32800);
    assert.equal(Object.hasOwn(reply, "result"), false);
    assert.deepEqual(failures, []);
    const next = client.request("still-alive");
    child.stdout.emit("data", frame({ id: 1, result: true }));
    assert.equal(await next, true);
  } finally {
    await watcher.close();
    rmSync(directory, { recursive: true, force: true });
  }
});

test("an unrelated handler error cannot bypass failure with a cancellation code", async () => {
  const error = Object.assign(new Error("unrelated failure"), { code: -32800 });
  const failures = [];
  const { child, client } = fixture(
    async () => {
      throw error;
    },
    (error) => failures.push(error),
  );
  child.stdout.emit(
    "data",
    frame({ id: "registration", method: "client/registerCapability", params: {} }),
  );
  await new Promise((resolve) => setImmediate(resolve));
  assert.deepEqual(failures, [error]);
  await assert.rejects(client.request("after-failure"), /unrelated failure/);
});

function nativeRegistrationFixture(t) {
  const directory = mkdtempSync(join(tmpdir(), "native-registration-"));
  const child = new EventEmitter();
  child.pid = 12345;
  child.stdin = new EventEmitter();
  child.stdout = new EventEmitter();
  child.stderr = new EventEmitter();
  child.stderr.setEncoding = () => {};
  const events = new EventEmitter();
  const messages = [];
  const watchers = [];
  const blocked = new Map();
  const closing = new Set();
  const waitFor = async (find) => {
    for (;;) {
      const found = find();
      if (found !== undefined) return found;
      await once(events, "change");
    }
  };
  let ended = false;
  function end(code, signal) {
    if (ended) return;
    ended = true;
    if (signal) for (const gate of blocked.values()) gate.release();
    queueMicrotask(() => {
      child.emit("exit", code, signal);
      child.emit("close", code, signal);
    });
  }
  child.kill = () => end(null, "SIGTERM");
  child.stdin.write = (bytes) => {
    const message = JSON.parse(bytes.toString().split("\r\n\r\n")[1]);
    messages.push(message);
    events.emit("change");
    if (message.method === "initialize")
      queueMicrotask(() =>
        child.stdout.emit(
          "data",
          frame({
            id: message.id,
            result: { serverInfo: { name: "typescript-go" }, capabilities: {} },
          }),
        ),
      );
    if (message.method === "shutdown")
      queueMicrotask(() => child.stdout.emit("data", frame({ id: message.id, result: null })));
    if (message.method === "exit") end(0, null);
  };
  t.mock.method(childProcess, "spawn", (executable, args) => {
    assert.equal(executable, "native-registration-fixture");
    assert.deepEqual(args, ["--lsp", "--stdio"]);
    return child;
  });
  t.mock.method(process, "kill", (pid, signal) => {
    assert.equal(pid, -child.pid);
    assert.equal(signal, "SIGKILL");
    end(null, signal);
  });
  t.mock.method(FSWatcher.prototype, "add", function () {
    watchers.push(this);
    events.emit("change");
    return this;
  });
  const close = FSWatcher.prototype.close;
  t.mock.method(FSWatcher.prototype, "close", function () {
    const result = close.call(this);
    closing.add(this);
    events.emit("change");
    return Promise.all([result, blocked.get(this)?.promise]);
  });
  syncBuiltinESMExports();
  t.after(() => {
    t.mock.restoreAll();
    syncBuiltinESMExports();
    rmSync(directory, { recursive: true, force: true });
  });
  const reply = (id) => messages.find((message) => message.id === id);
  return {
    messages,
    watchers,
    directory,
    run: (callback, signal) =>
      withNativeSession(
        { executable: "native-registration-fixture", cwd: directory, signal },
        callback,
      ),
    register(id, registrationIds = ["generated"]) {
      child.stdout.emit(
        "data",
        frame({
          id,
          method: "client/registerCapability",
          params: {
            registrations: registrationIds.map((registrationId) => ({
              id: registrationId,
              method: "workspace/didChangeWatchedFiles",
              registerOptions: { watchers: [{ globPattern: directory + "/**/*" }] },
            })),
          },
        }),
      );
    },
    unregister(id) {
      child.stdout.emit(
        "data",
        frame({
          id,
          method: "client/unregisterCapability",
          params: { unregisterations: [{ id: "generated" }] },
        }),
      );
    },
    watcher: (index) => waitFor(() => watchers[index]),
    closed: (watcher) => waitFor(() => closing.has(watcher) || undefined),
    reply: (id) => waitFor(() => reply(id)),
    hasReply: (id) => reply(id) !== undefined,
    blockClose(watcher) {
      let release;
      const promise = new Promise((resolve) => {
        release = resolve;
      });
      blocked.set(watcher, { promise, release });
      return release;
    },
  };
}

test("generated refresh replaces a completed same-ID registration instead of aborting (#233)", async (t) => {
  const native = nativeRegistrationFixture(t);
  await native.run(async () => {
    native.register("first");
    const first = await native.watcher(0);
    const stale = first.listeners("all")[0];
    first.emit("ready");
    assert.equal((await native.reply("first")).result, null);
    native.register("replacement");
    const replacement = await native.watcher(1);
    assert.equal(first.closed, true);
    const count = native.messages.length;
    stale("change", join(native.directory, "stale.ts"));
    assert.equal(native.messages.length, count);
    replacement.emit("ready");
    assert.equal((await native.reply("replacement")).result, null);
    replacement.emit("all", "change", join(native.directory, "generated.ts"));
    assert.equal(native.messages.at(-1).method, "workspace/didChangeWatchedFiles");
    native.unregister("remove");
    assert.equal((await native.reply("remove")).result, null);
    assert.equal(replacement.closed, true);
  });
  assert.ok(native.watchers.every((watcher) => watcher.closed));
});

test("pending replacement and stale unregister completion preserve the newest registration (#233)", async (t) => {
  const native = nativeRegistrationFixture(t);
  await native.run(async () => {
    native.register("pending");
    const first = await native.watcher(0);
    const releaseFirst = native.blockClose(first);
    native.register("replacement");
    const second = await native.watcher(1);
    assert.equal(first.closed, true);
    assert.equal((await native.reply("pending")).error.code, -32800);
    second.emit("ready");
    assert.equal(native.hasReply("replacement"), false);
    releaseFirst();
    assert.equal((await native.reply("replacement")).result, null);
    const releaseSecond = native.blockClose(second);
    native.unregister("remove");
    await native.closed(second);
    native.register("newest");
    const newest = await native.watcher(2);
    newest.emit("ready");
    releaseSecond();
    assert.equal((await native.reply("remove")).result, null);
    assert.equal((await native.reply("newest")).result, null);
    assert.equal(newest.closed, false);
    newest.emit("all", "change", join(native.directory, "newest.ts"));
    assert.equal(native.messages.at(-1).method, "workspace/didChangeWatchedFiles");
  });
  assert.ok(native.watchers.every((watcher) => watcher.closed));
});

test("shutdown deactivates replaced coverage before a prior close completion settles", async (t) => {
  const native = nativeRegistrationFixture(t);
  let release;
  let nativeCloserCalled = false;
  try {
    await native.run(async () => {
      native.register("first");
      const first = await native.watcher(0);
      first._addPathCloser(native.directory, () => {
        nativeCloserCalled = true;
      });
      first.emit("ready");
      assert.equal((await native.reply("first")).result, null);
      release = native.blockClose(first);
      native.register("replacement", ["generated", "late"]);
      const replacement = await native.watcher(1);
      replacement.emit("ready");
      assert.equal(nativeCloserCalled, true);
      assert.equal(first.closed, true);
      assert.equal(first.listenerCount("all"), 0);
      assert.equal(native.hasReply("replacement"), false);
    });
    release();
    await new Promise((resolve) => setImmediate(resolve));
    assert.equal(native.watchers.length, 2);
    assert.ok(
      native.watchers.every((watcher) => watcher.closed && watcher.listenerCount("all") === 0),
    );
    assert.equal(native.hasReply("replacement"), false);
  } finally {
    release?.();
  }
});

for (const cancel of [false, true]) {
  test(
    cancel
      ? "canceling a pending same-ID replacement closes both generations"
      : "failed required-input replacement remains fatal and is never acknowledged",
    async (t) => {
      const native = nativeRegistrationFixture(t);
      const controller = new AbortController();
      const failure = Object.assign(
        new Error(cancel ? "session canceled" : "required replacement watch denied"),
        { code: "EACCES" },
      );
      await assert.rejects(
        native.run(async () => {
          native.register("first");
          const first = await native.watcher(0);
          first.emit("ready");
          assert.equal((await native.reply("first")).result, null);
          native.register("replacement");
          const replacement = await native.watcher(1);
          if (cancel) controller.abort(failure);
          else replacement.emit("error", failure);
          await native.reply("replacement");
        }, controller.signal),
        (error) => error === failure,
      );
      assert.equal(native.watchers.length, 2);
      assert.ok(native.watchers.every((watcher) => watcher.closed));
      assert.equal(
        native.messages.some(
          (message) => message.id === "replacement" && Object.hasOwn(message, "result"),
        ),
        false,
      );
    },
  );
}

const permissionFixture = String.raw`
import assert from "node:assert/strict";
import fs, { accessSync, chmodSync, constants, mkdirSync, mkdtempSync, readFileSync, realpathSync, renameSync, rmSync, statSync, symlinkSync, unlinkSync, writeFileSync } from "node:fs";
import { syncBuiltinESMExports } from "node:module";
import { tmpdir } from "node:os";
import { basename, join } from "node:path";
import { fileURLToPath } from "node:url";
if (process.getuid() === 0) {
  process.env.TMPDIR = "/tmp";
  process.setgroups([]);
  process.setgid(65534);
  process.setuid(65534);
}
assert.notEqual(process.getuid(), 0);
const root = realpathSync(mkdtempSync(join(tmpdir(), "native-watch-permissions-")));
let pending;
let failure;
function expect(paths, type = 2) {
  if (failure) throw failure;
  return new Promise((resolve, reject) => {
    pending = { paths: new Set(paths), type, resolve, reject };
  });
}
function register(pattern) {
  return watchRegistration(
    { method: "workspace/didChangeWatchedFiles", registerOptions: { watchers: [{ globPattern: pattern }] } },
    (_, params) => {
      for (const event of params.changes)
        if (event.type === pending?.type) pending.paths.delete(fileURLToPath(event.uri));
      if (pending?.paths.size === 0) pending.resolve();
    },
    (error) => { failure = error; pending?.reject(error); },
  );
}
async function recovery() {
  const file = join(root, "denied.ts");
  const directory = join(root, "private");
  const nested = join(directory, "generated.ts");
  mkdirSync(directory);
  writeFileSync(file, "initial");
  writeFileSync(nested, "initial");
  const inodes = [statSync(file).ino, statSync(directory).ino];
  chmodSync(file, 0);
  chmodSync(directory, 0);
  assert.throws(() => accessSync(file, constants.R_OK), { code: "EACCES" });
  assert.throws(() => accessSync(directory, constants.X_OK), { code: "EACCES" });
  const watcher = register(root + "/**/*");
  try {
    await watcher.ready;
    let observed = expect([file, nested]);
    chmodSync(file, 0o600);
    chmodSync(directory, 0o700);
    await observed;
    assert.deepEqual([statSync(file).ino, statSync(directory).ino], inodes);
    observed = expect([file, nested]);
    writeFileSync(file, "restored file");
    writeFileSync(nested, "restored directory");
    await observed;
    const replacement = join(root, "replacement");
    writeFileSync(replacement, "replacement");
    chmodSync(replacement, 0);
    observed = expect([file]);
    renameSync(replacement, file);
    await observed;
    const replacementInode = statSync(file).ino;
    assert.notEqual(replacementInode, inodes[0]);
    observed = expect([file]);
    chmodSync(file, 0o600);
    await observed;
    assert.equal(statSync(file).ino, replacementInode);
    observed = expect([file]);
    writeFileSync(file, "restored replacement");
    await observed;
    assert.equal(failure, undefined);
  } finally {
    await watcher.close();
    chmodSync(file, 0o600);
    chmodSync(directory, 0o700);
  }
}
async function required() {
  async function denied(pattern) {
    failure = undefined;
    const watcher = register(pattern);
    try {
      await assert.rejects(watcher.ready, (error) => ["EACCES", "EPERM"].includes(error.code));
      assert.ok(["EACCES", "EPERM"].includes(failure.code));
    } finally {
      await watcher.close();
    }
  }
  const file = join(root, "required.ts");
  writeFileSync(file, "required");
  chmodSync(file, 0);
  await denied(file);
  const directory = join(root, "required");
  mkdirSync(directory);
  chmodSync(directory, 0);
  try { await denied(directory + "/**/*"); }
  finally { chmodSync(directory, 0o700); }
  const searchable = join(directory, "searchable");
  mkdirSync(searchable);
  writeFileSync(join(searchable, "known.ts"), "readable through an execute-only directory");
  chmodSync(searchable, 0o100);
  try {
    assert.equal(readFileSync(join(searchable, "known.ts"), "utf8"), "readable through an execute-only directory");
    await denied((process.platform === "darwin" ? searchable : directory) + "/**/*");
  } finally { chmodSync(searchable, 0o700); }
  rmSync(searchable, { recursive: true });
  const unsearchable = join(directory, "known.ts");
  writeFileSync(unsearchable, "required through an unsearchable directory");
  chmodSync(directory, 0o400);
  try {
    accessSync(directory, constants.R_OK);
    assert.throws(() => accessSync(directory, constants.X_OK), { code: "EACCES" });
    await denied(directory + "/**/*");
    await denied(unsearchable);
  } finally { chmodSync(directory, 0o700); }
  symlinkSync(file, join(directory, "external.ts"));
  await denied(directory + "/**/*");
  chmodSync(file, 0o600);
}
async function scanCoverage() {
  const directory = join(root, "metadata-private");
  const known = join(directory, "known.d.ts");
  mkdirSync(directory);
  writeFileSync(known, "export declare const value: 1;\n");
  const inode = statSync(directory).ino;
  chmodSync(directory, 0o400);
  accessSync(directory, constants.R_OK);
  assert.throws(() => accessSync(directory, constants.X_OK), { code: "EACCES" });
  assert.deepEqual(fs.readdirSync(directory), ["known.d.ts"]);
  assert.throws(() => fs.lstatSync(known), { code: "EACCES" });
  const nativeWatch = fs.watch;
  const handles = [];
  fs.watch = (path, options, listener) => {
    const handle = nativeWatch(path, options, listener);
    const record = { path, options, closed: false };
    const close = handle.close.bind(handle);
    handle.close = () => { record.closed = true; return close(); };
    handles.push(record);
    return handle;
  };
  syncBuiltinESMExports();
  const watcher = register(root + "/**/*");
  try {
    await watcher.ready;
    const parent = handles.find((handle) => handle.path === root);
    assert.equal(parent.options.recursive, true);
    let observed = expect([known]);
    chmodSync(directory, 0o700);
    await observed;
    assert.equal(statSync(directory).ino, inode);
    observed = expect([known]);
    writeFileSync(known, "export declare const value: 2;\n");
    await observed;
    const created = join(directory, "created.d.ts");
    observed = expect([created], 1);
    writeFileSync(created, "export declare const created: true;\n");
    await observed;
    assert.equal(readFileSync(known, "utf8"), "export declare const value: 2;\n");
    assert.equal(handles.filter((handle) => handle.path === root).length, 1);
    assert.equal(parent.closed, false);
    assert.equal(failure, undefined);
  } finally {
    await watcher.close();
    fs.watch = nativeWatch;
    syncBuiltinESMExports();
    chmodSync(directory, 0o700);
  }
  assert(handles.every((handle) => handle.closed));
}
async function nativeCoverage() {
  const ambiguous = join(root, basename(root));
  const generated = join(root, "generated");
  const known = join(generated, "known.d.ts");
  const deleted = join(generated, "first-delete.d.ts");
  mkdirSync(generated);
  writeFileSync(known, "export declare const value: 1;\n");
  writeFileSync(deleted, "export declare const deleted: true;\n");
  chmodSync(generated, 0o300);
  accessSync(generated, constants.X_OK | constants.W_OK);
  assert.throws(() => accessSync(generated, constants.R_OK), { code: "EACCES" });
  assert.equal(readFileSync(known, "utf8"), "export declare const value: 1;\n");
  const inode = statSync(known).ino;
  const nativeWatch = fs.watch;
  const handles = [];
  const deferred = [];
  let captureAmbiguous = false;
  let captured;
  const namedEvent = new Promise((resolve) => { captured = resolve; });
  fs.watch = (path, options, listener) => {
    const handle = nativeWatch(path, options, (...args) => {
      if (captureAmbiguous && path === root && args[1] === basename(root)) {
        deferred.push(() => listener(...args));
        captured();
      } else listener(...args);
    });
    const record = { path, options, closed: false };
    const close = handle.close.bind(handle);
    handle.close = () => { record.closed = true; return close(); };
    handles.push(record);
    return handle;
  };
  syncBuiltinESMExports();
  const stage = async (name, operation) => {
    process.stderr.write("native coverage (#233): waiting for " + name + "\n");
    await operation;
    process.stderr.write("native coverage (#233): completed " + name + "\n");
  };
  const watcher = register(root + "/**/*");
  try {
    process.stderr.write("native coverage (#233): waiting for ready\n");
    await watcher.ready;
    let observed = expect([ambiguous], 3);
    captureAmbiguous = true;
    writeFileSync(ambiguous, "deleted before named callback delivery");
    process.stderr.write("native coverage (#233): completed ready\n");
    await stage("same-basename native callback", Promise.race([namedEvent, observed]));
    assert.ok(deferred.length);
    unlinkSync(ambiguous);
    assert.equal(handles.some((handle) => handle.path === ambiguous), false);
    captureAmbiguous = false;
    for (const deliver of deferred.splice(0)) deliver();
    await stage("same-basename unseen deletion", observed);
    const parent = handles.find((handle) => handle.path === root);
    assert.equal(parent.options.recursive, true);
    observed = expect([deleted], 3);
    unlinkSync(deleted);
    await stage("execute-only first deletion", observed);
    observed = expect([known]);
    writeFileSync(known, "export declare const value: 2;\n");
    await stage("execute-only same-inode update", observed);
    assert.equal(statSync(known).ino, inode);
    assert.throws(() => accessSync(generated, constants.R_OK), { code: "EACCES" });
    observed = expect([known], 3);
    unlinkSync(known);
    await stage("known file deletion", observed);
    observed = expect([known], 1);
    writeFileSync(known, "export declare const value: 3;\n");
    await stage("known file recreation", observed);
    const recreated = statSync(known).ino;
    const replacement = join(generated, "replacement");
    writeFileSync(replacement, "export declare const value: 4;\n");
    observed = expect([known]);
    renameSync(replacement, known);
    await stage("known file replacement", observed);
    assert.notEqual(statSync(known).ino, recreated);
    observed = expect([generated]);
    chmodSync(generated, 0o700);
    await stage("directory permission restoration", observed);
    const created = join(generated, "created.d.ts");
    observed = expect([created], 1);
    writeFileSync(created, "export declare const created: true;\n");
    await stage("restored directory creation", observed);
    assert.equal(readFileSync(known, "utf8"), "export declare const value: 4;\n");
    assert.equal(handles.filter((handle) => handle.path === root).length, 1);
    assert.equal(parent.closed, false);
    assert.equal(failure, undefined);
  } finally {
    captureAmbiguous = false;
    for (const deliver of deferred.splice(0)) deliver();
    await stage("close", watcher.close());
    fs.watch = nativeWatch;
    syncBuiltinESMExports();
    chmodSync(generated, 0o700);
  }
  assert(handles.every((handle) => handle.closed));
}
try { await ACTION(); }
finally { rmSync(root, { recursive: true, force: true }); }
`;

for (const [action, title, platform] of [
  [
    "recovery",
    "ambient file and directory permission denials recover after same-inode chmod and replacement",
  ],
  ["required", "required literal files, roots and external symlinks fail closed"],
  [
    "scanCoverage",
    "Darwin covered directory scan survives denied child metadata and same-handle restoration (#233)",
    "darwin",
  ],
  [
    "nativeCoverage",
    "Darwin owned recursive watch preserves unseen deletion and execute-only generated refresh (#233)",
    "darwin",
  ],
]) {
  test(
    title,
    { skip: platform ? process.platform !== platform : process.platform === "win32" },
    async (t) => {
      const source =
        `import { watchRegistration } from ${JSON.stringify(new URL("./verify-editor.mjs", import.meta.url).href)};\n` +
        permissionFixture.replace("await ACTION()", `await ${action}()`);
      const child = spawn(process.execPath, ["--input-type=module", "-e", source], {
        signal: t.signal,
        stdio: ["ignore", "pipe", "pipe"],
      });
      let output = "";
      if (action === "nativeCoverage")
        child.stderr.on("data", (data) => process.stderr.write(data));
      for (const stream of [child.stdout, child.stderr])
        stream.on("data", (data) => (output += data));
      const code = await new Promise((resolve, reject) => {
        child.once("error", reject);
        child.once("close", resolve);
      });
      assert.equal(code, 0, output);
    },
  );
}

test("closing during initial recursion disposes the parent watch before the scan resumes", async (t) => {
  const directory = fs.realpathSync(mkdtempSync(join(tmpdir(), "native-watch-owned-parent-")));
  const watcher = new FSWatcher({ ignoreInitial: true });
  let scanned;
  let release;
  const entered = new Promise((resolve) => (scanned = resolve));
  const scan = new Promise((resolve) => (release = resolve));
  const nativeWatch = fs.watch;
  let closed;
  t.mock.method(fs, "watch", (path, ...args) => {
    const handle = nativeWatch(path, ...args);
    closed = new Promise((resolve) => handle.once("close", resolve));
    return handle;
  });
  syncBuiltinESMExports();
  t.mock.method(watcher._nodeFsHandler, "_handleRead", () => {
    assert.ok(closed, "directory scanning began before its parent subscription");
    scanned();
    return scan;
  });
  try {
    watcher.add(directory);
    await entered;
    await watcher.close();
    await closed;
    release();
    assert.deepEqual(watcher.getWatched(), {});
  } finally {
    release();
    await watcher.close();
    t.mock.restoreAll();
    syncBuiltinESMExports();
    rmSync(directory, { recursive: true, force: true });
  }
});

test(
  "Darwin file-to-directory replacement preserves shared recursive coverage and rejects unidentified events (#233)",
  { skip: process.platform !== "darwin" },
  async (t) => {
    const directory = fs.realpathSync(mkdtempSync(join(tmpdir(), "native-watch-replacement-")));
    const target = join(directory, "generated");
    const child = join(target, "known.ts");
    writeFileSync(target, "original file");
    const nativeWatch = fs.watch;
    const handles = [];
    const deferred = [];
    let replacing = false;
    t.mock.method(fs, "watch", (path, options, listener) => {
      const handle = nativeWatch(path, options, (...args) => {
        if (replacing && (path === directory || !options.recursive))
          deferred.push(() => listener(...args));
        else listener(...args);
      });
      handles.push({ path, options, listener, handle });
      return handle;
    });
    syncBuiltinESMExports();
    const watchers = [];
    const events = [];
    const failures = [];
    let pending;
    let failed;
    const failure = new Promise((resolve) => (failed = resolve));
    const register = (globPattern) => {
      const watcher = watchRegistration(
        {
          method: "workspace/didChangeWatchedFiles",
          registerOptions: { watchers: [{ globPattern }] },
        },
        (_, params) => {
          events.push(...params.changes);
          for (const event of params.changes)
            if (event.type === 2) pending?.paths.delete(fileURLToPath(event.uri));
          if (pending?.paths.size === 0) pending.resolve();
        },
        (error) => {
          failures.push(error);
          pending?.reject(error);
          failed(error);
        },
      );
      watchers.push(watcher);
      return watcher;
    };
    try {
      await register(target).ready;
      const original = handles.find((handle) => handle.path === target);
      assert.equal(original.options.recursive, false);
      replacing = true;
      unlinkSync(target);
      mkdirSync(target);
      writeFileSync(child, "initial generated source");
      await register(target + "/**/*").ready;
      const recursive = handles.find(
        (handle) => handle.path === target && handle.options.recursive,
      );
      assert.ok(recursive);
      original.listener("rename", null);
      const observed = new Promise((resolve, reject) => {
        pending = { paths: new Set([target, child]), resolve, reject };
      });
      writeFileSync(child, "updated generated source");
      await observed;
      pending = undefined;
      assert.deepEqual(failures, []);
      assert.equal(readFileSync(child, "utf8"), "updated generated source");
      assert.equal(handles.filter((handle) => handle.path === target).length, 2);
      recursive.listener("change", null);
      assert.match((await failure).message, /native recursive watch omitted the changed path/);
      assert.equal(failures.length, 1);
      for (const watcher of watchers) await watcher.close();
      const count = events.length;
      const subscriptions = handles.length;
      for (const deliver of deferred) deliver();
      for (const handle of handles) handle.listener("change", null);
      await new Promise((resolve) => setImmediate(resolve));
      assert.equal(events.length, count);
      assert.equal(handles.length, subscriptions);
      assert.equal(failures.length, 1);
    } finally {
      for (const watcher of watchers) await watcher.close();
      t.mock.restoreAll();
      syncBuiltinESMExports();
      rmSync(directory, { recursive: true, force: true });
    }
  },
);

for (const code of ["EACCES", "UNKNOWN"]) {
  test(`${code} creation requires actual coverage and asynchronous shared watcher errors remain fatal`, async (t) => {
    const directory = fs.realpathSync(mkdtempSync(join(tmpdir(), "native-watch-fatal-")));
    const file = join(directory, "readable.ts");
    writeFileSync(file, "readable");
    const nativeWatch = fs.watch;
    const denied = Object.assign(new Error("watch denied"), { code, path: file });
    t.mock.method(fs, "watch", (path, ...args) => {
      if (path === file) throw denied;
      return nativeWatch(path, ...args);
    });
    syncBuiltinESMExports();
    const pattern = {
      method: "workspace/didChangeWatchedFiles",
      registerOptions: { watchers: [{ globPattern: directory + "/**/*" }] },
    };
    const failures = [];
    const watchers = [];
    try {
      let changed;
      const observed = new Promise((resolve) => (changed = resolve));
      const first = watchRegistration(
        pattern,
        (_, params) => {
          if (params.changes.some((event) => fileURLToPath(event.uri) === file && event.type === 2))
            changed();
        },
        (error) => failures.push(error),
      );
      watchers.push(first);
      if (code === "EACCES" && process.platform === "darwin") {
        await first.ready;
        writeFileSync(file, "changed through the owned recursive ancestor");
        await observed;
        assert.deepEqual(failures, []);
      } else {
        await assert.rejects(first.ready, (error) => error === denied);
        assert.deepEqual(failures, [denied]);
      }
      assert.equal(denied.message, "watch denied");
      const message = denied.message;
      await first.close();
      t.mock.restoreAll();
      let fileWatcher;
      t.mock.method(fs, "watch", (path, ...args) => {
        const handle = nativeWatch(path, ...args);
        if (path === file) fileWatcher = handle;
        return handle;
      });
      syncBuiltinESMExports();
      failures.length = 0;
      for (let subscriber = 0; subscriber < 2; subscriber++) {
        const watcher = watchRegistration(
          pattern,
          () => {},
          (error) => failures.push(error),
        );
        watchers.push(watcher);
        await watcher.ready;
      }
      fs.chmodSync(file, 0);
      fileWatcher.emit("error", denied);
      assert.deepEqual(failures, [denied, denied]);
      assert.equal(denied.message, message);
    } finally {
      for (const watcher of watchers) await watcher.close();
      t.mock.restoreAll();
      syncBuiltinESMExports();
      fs.chmodSync(file, 0o600);
      rmSync(directory, { recursive: true, force: true });
    }
  });
}
