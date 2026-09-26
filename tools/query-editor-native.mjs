import assert from "node:assert/strict";
import { mkdtempSync, readFileSync, realpathSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";

import { queryNative, verify, withNativeSession, withCommandSignals } from "./verify-editor.mjs";

const [executable, cwd, file, project, definition, expectedProcesses] = process.argv.slice(2);
if (!definition)
  throw new Error(
    "usage: query-editor-native <compiler> <workspace> <file> <project> <definition>",
  );
await withCommandSignals(async (signal) => {
  const source = readFileSync(resolve(cwd, file), "utf8");
  const offset = source.indexOf("generatedValue");
  assert.ok(offset >= 0);
  const before = source.slice(0, offset);
  const position = {
    line: before.split("\n").length - 1,
    character: offset - before.lastIndexOf("\n") - 1,
  };
  const results = [];
  let owned = [];
  let processes = [];
  function descendants(pid) {
    const children = readFileSync(`/proc/${pid}/task/${pid}/children`, "utf8")
      .trim()
      .split(/\s+/)
      .filter(Boolean)
      .map(Number);
    return children.flatMap((child) => [child, ...descendants(child)]);
  }
  function stopped(pids) {
    for (const pid of pids) {
      try {
        const stat = readFileSync(`/proc/${pid}/stat`, "utf8");
        assert.equal(
          stat.slice(stat.lastIndexOf(")") + 2).split(" ")[0],
          "Z",
          `native process ${pid} still executing`,
        );
      } catch (error) {
        if (error.code !== "ENOENT") throw error;
      }
    }
  }
  await withNativeSession({ executable, cwd, signal }, async (session) => {
    if (process.platform === "linux") {
      owned = descendants(process.pid);
      processes = owned.map((pid) => ({ pid, executable: realpathSync(`/proc/${pid}/exe`) }));
      if (expectedProcesses) assert.equal(owned.length, Number(expectedProcesses));
    }
    for (const operation of ["diagnostics", "definition", "hover", "references"]) {
      const result = await session.query({ file, operation, position, includeDeclaration: true });
      assert.equal(resolve(result.project.configFilePath), resolve(cwd, project));
      results.push(result);
    }
  });
  if (process.platform === "linux") stopped(owned);
  assert.deepEqual(results[0].result, []);
  assert.ok(
    results[1].result.some(
      (entry) => realpathSync(entry.file) === realpathSync(resolve(cwd, definition)),
    ),
  );
  assert.ok(JSON.stringify(results[2].result).includes("generatedValue"));
  assert.ok(results[3].result.some((entry) => resolve(entry.file) === resolve(cwd, file)));

  if (process.platform === "linux") {
    const controller = new AbortController();
    await assert.rejects(
      withNativeSession(
        { executable, cwd, signal: AbortSignal.any([signal, controller.signal]) },
        async (session) => {
          owned = descendants(process.pid);
          const pending = session.query({ file, operation: "hover", position });
          controller.abort(new Error("native query fixture cancellation"));
          return pending;
        },
      ),
      /fixture cancellation/,
    );
    stopped(owned);
  }
  process.stdout.write(
    JSON.stringify(
      {
        executable,
        processes,
        results,
        cancellation:
          process.platform === "linux"
            ? "native descendants stopped and pipes closed"
            : "not exercised",
      },
      null,
      2,
    ) + "\n",
  );

  const parityRoot = mkdtempSync(join(tmpdir(), "native-editor-parity-"));
  const previousCwd = process.cwd();
  try {
    writeFileSync(
      join(parityRoot, "tsconfig.json"),
      JSON.stringify({
        compilerOptions: { strict: true, target: "ES2022", jsx: "preserve", types: [] },
        files: ["view.tsx", "legacy.ts"],
      }),
    );
    writeFileSync(
      join(parityRoot, "legacy.ts"),
      "export interface Current { value: string; }\n/** @deprecated Use Current. */\nexport interface Legacy extends Current {}\n",
    );
    writeFileSync(
      join(parityRoot, "view.tsx"),
      'import type { Legacy } from "./legacy";\ndeclare global { namespace JSX { interface IntrinsicElements { div: {}; } } }\nexport const one: Legacy = { value: "a" };\nexport const two: Legacy = { value: "b" };\nexport const three: Legacy = { value: "c" };\nexport const four: Legacy = { value: "d" };\nexport const view = <div />;\n',
    );
    const probes = join(parityRoot, "probes.json");
    writeFileSync(
      probes,
      JSON.stringify([
        { file: "view.tsx", symbol: "./legacy", project: "tsconfig.json", definition: "legacy.ts" },
      ]),
    );
    process.chdir(parityRoot);
    await verify(executable, probes, { signal });
    const hints = await queryNative(
      { executable, cwd: parityRoot, signal },
      { file: "view.tsx", operation: "diagnostics" },
    );
    assert.ok(hints.result.every((diagnostic) => diagnostic.severity !== 1));
    process.stdout.write(
      JSON.stringify(
        {
          case: "actual TSX query preserves raw diagnostics with clean CLI sanity",
          ...hints,
        },
        null,
        2,
      ) + "\n",
    );
  } finally {
    process.chdir(previousCwd);
    rmSync(parityRoot, { recursive: true, force: true });
  }
});
