import assert from "node:assert/strict";
import { spawn, spawnSync } from "node:child_process";
import { mkdtempSync, readFileSync, writeFileSync, rmSync, watch, realpathSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
import test from "node:test";
import { queryNative, withNativeSession } from "./verify-editor.mjs";

function fixture(options = {}) {
  const cwd = mkdtempSync(join(tmpdir(), "native query é "));
  const executable = join(cwd, "compiler");
  const events = join(cwd, "events");
  const project = join(cwd, "tsconfig.json");
  const source = join(cwd, "view.tsx");
  const server = fileURLToPath(new URL("./testdata/native-lsp-process.mjs", import.meta.url));
  const quote = (value) => "'" + value.replaceAll("'", "'\\''") + "'";
  writeFileSync(executable, `#!/bin/sh\nexec ${quote(process.execPath)} ${quote(server)} "$@"\n`, {
    mode: 0o755,
  });
  writeFileSync(project, "{}");
  writeFileSync(source, "export const value = 1;\n");
  writeFileSync(events, "");
  const keys = { EDITOR_TEST_EVENTS: events, EDITOR_TEST_PROJECT: project, ...options };
  const original = Object.fromEntries(Object.keys(keys).map((key) => [key, process.env[key]]));
  Object.assign(process.env, keys);
  return {
    cwd,
    executable,
    events,
    source,
    records: () =>
      readFileSync(events, "utf8")
        .trim()
        .split("\n")
        .filter(Boolean)
        .map((line) => JSON.parse(line)),
    close() {
      for (const [key, value] of Object.entries(original)) {
        if (value === undefined) delete process.env[key];
        else process.env[key] = value;
      }
      rmSync(cwd, { recursive: true, force: true });
    },
  };
}

function assertStopped(records) {
  for (const { pid } of records.filter((record) => record.pid)) {
    try {
      const stat = readFileSync(`/proc/${pid}/stat`, "utf8");
      assert.equal(
        stat.slice(stat.lastIndexOf(")") + 2).split(" ")[0],
        "Z",
        `process ${pid} still executing`,
      );
    } catch (error) {
      if (error.code !== "ENOENT") throw error;
    }
  }
}

test(
  "query preserves hints and finishes native shutdown without CLI compilation",
  { skip: process.platform !== "linux" },
  async () => {
    const f = fixture();
    try {
      const result = await queryNative(f, { file: f.source, operation: "diagnostics" });
      assert.equal(result.result[0].severity, 4);
      assert.equal(result.result[0].code, 6385);
      const records = f.records();
      assert.deepEqual(
        records.filter((record) => record.args).map((record) => record.args),
        [["--lsp", "--stdio"]],
      );
      assert.deepEqual(
        records
          .filter((record) => record.method)
          .slice(-2)
          .map((record) => record.method),
        ["shutdown", "exit"],
      );
      assert.equal(
        records.find((record) => record.method === "textDocument/didOpen").params.textDocument
          .languageId,
        "typescriptreact",
      );
      assertStopped(records);
    } finally {
      f.close();
    }
  },
);

test("pre-aborted queries cannot start a compiler", async () => {
  const controller = new AbortController();
  controller.abort(new Error("caller canceled"));
  await assert.rejects(
    queryNative({ executable: "/does-not-exist", cwd: tmpdir(), signal: controller.signal }, {}),
    /caller canceled/,
  );
});

test(
  "canceling a pending query terminates launcher and native descendant before returning",
  { skip: process.platform !== "linux" },
  async () => {
    const f = fixture();
    const prior = process.env.EDITOR_TEST_WRAPPER;
    process.env.EDITOR_TEST_WRAPPER = "1";
    try {
      const controller = new AbortController();
      await assert.rejects(
        withNativeSession({ ...f, signal: controller.signal }, async (session) => {
          const pending = session.query({ file: f.source, operation: "diagnostics" });
          controller.abort(new Error("caller canceled"));
          return pending;
        }),
        /caller canceled/,
      );
      assert.equal(f.records().filter((record) => record.pid).length, 2);
      assertStopped(f.records());
    } finally {
      if (prior === undefined) delete process.env.EDITOR_TEST_WRAPPER;
      else process.env.EDITOR_TEST_WRAPPER = prior;
      f.close();
    }
  },
);

test(
  "CLI SIGTERM stays active during shutdown and closes the native process group",
  { skip: process.platform !== "linux" },
  async () => {
    const f = fixture();
    let child;
    const watcher = watch(f.events, () => {
      if (f.records().some((record) => record.method === "shutdown")) child?.kill("SIGTERM");
    });
    try {
      child = spawn(
        process.execPath,
        [
          fileURLToPath(new URL("./query-editor.mjs", import.meta.url)),
          f.executable,
          f.cwd,
          JSON.stringify({ file: f.source, operation: "diagnostics" }),
        ],
        {
          env: { ...process.env, EDITOR_TEST_WRAPPER: "1", EDITOR_TEST_HOLD_SHUTDOWN: "1" },
          stdio: ["ignore", "pipe", "pipe"],
        },
      );
      let stdout = "";
      let stderr = "";
      child.stdout.on("data", (chunk) => {
        stdout += chunk;
      });
      child.stderr.on("data", (chunk) => {
        stderr += chunk;
      });
      const status = await new Promise((resolve, reject) => {
        child.once("error", reject);
        child.once("close", (code, signal) => resolve({ code, signal }));
      });
      assert.deepEqual(status, { code: 1, signal: null });
      assert.equal(stdout, "");
      assert.match(stderr, /SIGTERM/);
      assertStopped(f.records());
    } finally {
      watcher.close();
      f.close();
    }
  },
);

test(
  "canceling initialization rejects and closes compiler pipes",
  { skip: process.platform !== "linux" },
  async () => {
    const f = fixture();
    try {
      const controller = new AbortController();
      const pending = queryNative(
        { ...f, signal: controller.signal },
        { file: f.source, operation: "diagnostics" },
      );
      controller.abort(new Error("initialization canceled"));
      await assert.rejects(pending, /initialization canceled/);
      assertStopped(f.records());
    } finally {
      f.close();
    }
  },
);

test("spawn failure rejects without waiting for nonexistent process shutdown", async () => {
  await assert.rejects(
    queryNative(
      { executable: "/does-not-exist", cwd: tmpdir() },
      { file: "unused.ts", operation: "diagnostics" },
    ),
    /ENOENT/,
  );
});

test(
  "verifier cancellation during CLI checking closes both compiler and language server groups",
  { skip: process.platform !== "linux" },
  async () => {
    const f = fixture();
    const probes = join(f.cwd, "probes.json");
    writeFileSync(
      probes,
      JSON.stringify([{ file: f.source, symbol: "value", definitionAbsent: true }]),
    );
    let child;
    const watcher = watch(f.events, () => {
      if (f.records().some((record) => record.args?.includes("--noEmit") && !record.wrapper))
        child?.kill("SIGTERM");
    });
    try {
      child = spawn(
        process.execPath,
        [fileURLToPath(new URL("./verify-editor-cli.mjs", import.meta.url)), f.executable, probes],
        {
          cwd: f.cwd,
          env: { ...process.env, EDITOR_TEST_WRAPPER: "1", EDITOR_TEST_HOLD_CHECK: "1" },
          stdio: ["ignore", "pipe", "pipe"],
        },
      );
      let stderr = "";
      child.stdout.resume();
      child.stderr.on("data", (chunk) => {
        stderr += chunk;
      });
      const status = await new Promise((resolve, reject) => {
        child.once("error", reject);
        child.once("close", (code, signal) => resolve({ code, signal }));
      });
      assert.deepEqual(status, { code: 1, signal: null });
      assert.match(stderr, /SIGTERM/);
      assert.ok(f.records().some((record) => record.args?.includes("--noEmit") && !record.wrapper));
      assertStopped(f.records());
    } finally {
      watcher.close();
      f.close();
    }
  },
);

test(
  "acknowledged shutdown tolerates canceled background registration but rejects real failures",
  { skip: process.platform !== "linux" },
  async () => {
    for (const [stderr, status, failShutdown, succeeds, watchCount = 1] of [
      ["context canceled\n", "1", "0", true],
      ["context canceled\n", "1", "0", true, 3],
      ["context canceled\nother failure\n", "1", "0", false],
      ["other failure\n", "1", "0", false],
      ["context canceled\n", "2", "0", false],
      ["context canceled\n", "1", "1", false],
    ]) {
      const f = fixture({
        EDITOR_TEST_EXIT_STDERR: stderr,
        EDITOR_TEST_EXIT_STATUS: status,
        EDITOR_TEST_FAIL_SHUTDOWN: failShutdown,
        EDITOR_TEST_WATCH_ON_SHUTDOWN: String(watchCount),
      });
      try {
        const query = queryNative(f, { file: f.source, operation: "diagnostics" });
        if (succeeds) {
          assert.equal((await query).result[0].code, 6385);
          assert.equal(f.records().filter((entry) => entry.lateWatch).length, watchCount);
          assert.ok(f.records().some((entry) => entry.method === "exit"));
        } else await assert.rejects(query, /native LSP exited/);
        assertStopped(f.records());
      } finally {
        f.close();
      }
    }
  },
);

test("persistent session completes queries and synchronizes edits without another process", async () => {
  const f = fixture();
  try {
    await withNativeSession(f, async (session) => {
      const query = {
        file: f.source,
        operation: "completion",
        position: { line: 0, character: 1 },
      };
      assert.equal((await session.query(query)).result.items[0].label, "generatedMember");
      writeFileSync(f.source, "export const value = 2;\n");
      await session.query(query);
      const records = f.records();
      assert.equal(records.filter((entry) => entry.args).length, 1);
      assert.equal(records.filter((entry) => entry.method === "initialize").length, 1);
      assert.equal(
        records.find((entry) => entry.method === "textDocument/didChange").params.textDocument
          .version,
        2,
      );
    });
  } finally {
    f.close();
  }
});

test("native process death rejects a persistent callback waiting for its owner", async () => {
  const f = fixture();
  try {
    await assert.rejects(
      withNativeSession(f, async (session) => {
        process.kill(session.processId, "SIGKILL");
        await new Promise(() => {});
      }),
      /native LSP exited/,
    );
    assertStopped(f.records());
  } finally {
    f.close();
  }
});

test("module lookup imports the shared runtime outside the checkout without ambient dependencies", () => {
  const cwd = mkdtempSync(join(tmpdir(), "native-module-import-"));
  try {
    const cli = fileURLToPath(new URL("./query-editor.mjs", import.meta.url));
    const result = spawnSync(process.execPath, [cli, "/missing/compiler", cwd, "--module-path"], {
      cwd,
      encoding: "utf8",
      env: { ...process.env, NODE_PATH: "" },
    });
    assert.equal(result.status, 0, result.stderr);
    assert.equal(
      result.stdout.trim(),
      realpathSync(fileURLToPath(new URL("./verify-editor.mjs", import.meta.url))),
    );
    const imported = spawnSync(
      process.execPath,
      [
        "--input-type=module",
        "-e",
        'const runtime = await import(process.argv[1]); if(typeof runtime.withNativeSession !== "function") process.exit(1);',
        pathToFileURL(result.stdout.trim()).href,
      ],
      { cwd, encoding: "utf8", env: { ...process.env, NODE_PATH: "" } },
    );
    assert.equal(imported.status, 0, imported.stderr);
  } finally {
    rmSync(cwd, { recursive: true, force: true });
  }
});

test("malformed transport after acknowledged shutdown cannot become a canceled-exit success", async () => {
  const f = fixture({
    EDITOR_TEST_EXIT_STDERR: "context canceled\n",
    EDITOR_TEST_EXIT_STATUS: "1",
    EDITOR_TEST_MALFORMED_EXIT: "1",
  });
  try {
    await assert.rejects(queryNative(f, { file: f.source, operation: "diagnostics" }), SyntaxError);
    assert.ok(f.records().some((entry) => entry.method === "exit"));
    assertStopped(f.records());
  } finally {
    f.close();
  }
});
