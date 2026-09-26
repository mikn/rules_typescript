import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { readFileSync, writeFileSync, realpathSync, existsSync, writeSync } from "node:fs";
import { resolve } from "node:path";
import { runCompiler, withCommandSignals, withNativeSession } from "./verify-editor.mjs";

const [executable, cwd, bazel] = process.argv.slice(2);
const input = resolve(cwd, "generated/names.json");
const consumer = resolve(cwd, "generated/consumer.ts");
const originalInput = readFileSync(input);
const originalConsumer = readFileSync(consumer);
const generated = resolve(cwd, "bazel-bin/generated/generated/value.ts");
const treeGenerated = resolve(cwd, "bazel-bin/generated/generated/types/first.d.ts");
const projections = [
  { object: "generatedMembers", file: generated },
  { object: "generatedTreeMembers", file: treeGenerated },
];
const reports = [];
function generatedHash(file = generated) {
  return existsSync(file) ? createHash("sha256").update(readFileSync(file)).digest("hex") : null;
}
function evidence(record) {
  writeSync(
    2,
    JSON.stringify({
      liveEditorEvidence: true,
      generatedTreeHash: generatedHash(treeGenerated),
      ...record,
    }) + "\n",
  );
}

await withCommandSignals(async (signal) => {
  async function refresh(phase) {
    evidence({ phase, event: "refresh-start", generatedHash: generatedHash() });
    const result = await runCompiler(
      bazel,
      [
        "run",
        "--run_validations=false",
        "--output_groups=-_validation",
        "//generated:refresh_generated",
      ],
      signal,
      cwd,
    );
    evidence({ phase, event: "refresh-end", generatedHash: generatedHash(), ...result });
    assert.equal(result.status, 0, result.stderr);
  }
  async function compilerSanity(expectedError) {
    const result = await runCompiler(
      executable,
      [
        "--project",
        resolve(cwd, "generated/.bazel/tsconfig/generated_test.json"),
        "--noEmit",
        "--pretty",
        "false",
      ],
      signal,
      cwd,
    );
    if (expectedError) {
      assert.ok(result.status === 1 || result.status === 2, result.stderr);
      const output = result.stdout + result.stderr;
      const errors = [...output.matchAll(/^(.+)\((\d+),(\d+)\): error TS(\d+):/gm)];
      assert.equal([...output.matchAll(/error TS\d+:/g)].length, projections.length, output);
      assert.equal(errors.length, projections.length, output);
      const actual = errors.map(([, file, line, character, code]) => ({
        file: resolve(cwd, file),
        line: Number(line),
        character: Number(character),
        code: Number(code),
      }));
      const expected = projections.map(({ object }) => {
        const position = positionOf("addedMember", object);
        return {
          file: consumer,
          line: position.line + 1,
          character: position.character + 1,
          code: 2339,
        };
      });
      assert.deepEqual(actual, expected);
    } else assert.equal(result.status, 0, result.stdout + result.stderr);
    reports.push({ compilerSanity: true, expectedError, ...result });
  }
  const liveSource =
    originalConsumer.toString() +
    '\nimport { generatedMembers } from "#generated/value";\nexport const liveFirst: string = generatedMembers.first;\nexport const liveAdded: string = generatedMembers.addedMember;\nimport { generatedTreeMembers } from "#types/first";\nexport const liveTreeFirst: string = generatedTreeMembers.first;\nexport const liveTreeAdded: string = generatedTreeMembers.addedMember;\n';
  writeFileSync(consumer, liveSource);
  function positionOf(member, object = "generatedMembers") {
    const offset = liveSource.indexOf(object + "." + member) + object.length + 1;
    const before = liveSource.slice(0, offset);
    return {
      line: before.split("\n").length - 1,
      character: offset - before.lastIndexOf("\n") - 1,
    };
  }
  try {
    await withNativeSession({ executable, cwd, signal }, async (session) => {
      const pid = session.processId;
      async function observe(phase, member, present, missingAdded, projection) {
        const position = positionOf(member, projection.object);
        let previous;
        evidence({ phase, event: "observe-start", member, present, missingAdded, pid, position });
        for (;;) {
          signal.throwIfAborted();
          const completion = await session.query({
            file: consumer,
            operation: "completion",
            position,
          });
          const labels = (
            Array.isArray(completion.result) ? completion.result : completion.result?.items || []
          ).map((entry) => entry.label);
          const diagnostics = await session.query({ file: consumer, operation: "diagnostics" });
          const errors = diagnostics.result.filter((entry) => entry.severity === 1);
          const missingPositions = projections.map(({ object }) =>
            positionOf("addedMember", object),
          );
          const expectedErrors = missingAdded
            ? errors.length === projections.length &&
              missingPositions.every((position) =>
                errors.some(
                  (error) =>
                    error.code === 2339 &&
                    error.range.start.line === position.line &&
                    error.range.start.character === position.character &&
                    error.range.end.line === position.line &&
                    error.range.end.character === position.character + "addedMember".length,
                ),
              )
            : errors.length === 0;
          const observation = {
            phase,
            event: "predicate",
            member,
            projection,
            labels,
            expectedPresent: present,
            actualPresent: labels.includes(member),
            errors,
            expectedMissingMember: missingAdded
              ? { code: 2339, positions: missingPositions, length: "addedMember".length }
              : null,
            expectedErrors,
            actualProject: completion.project.configFilePath,
            expectedProject: resolve(cwd, "generated/.bazel/tsconfig/generated_test.json"),
            pid: session.processId,
            samePid: session.processId === pid,
            generatedHash: generatedHash(),
          };
          const current = JSON.stringify(observation);
          if (current !== previous) evidence(observation);
          previous = current;
          if (labels.includes(member) === present && expectedErrors) {
            assert.equal(session.processId, pid);
            assert.equal(readFileSync(consumer, "utf8"), liveSource);
            assert.equal(
              resolve(completion.project.configFilePath),
              resolve(cwd, "generated/.bazel/tsconfig/generated_test.json"),
            );
            const definition = await session.query({
              file: consumer,
              operation: "definition",
              position,
            });
            const hover = await session.query({ file: consumer, operation: "hover", position });
            if (present) {
              assert.ok(
                definition.result.some((entry) => {
                  if (
                    !existsSync(entry.file) ||
                    realpathSync(entry.file) !== realpathSync(projection.file)
                  )
                    return false;
                  const range = entry.targetSelectionRange || entry.range;
                  if (range.start.line !== range.end.line) return false;
                  const text = readFileSync(entry.file, "utf8")
                    .split("\n")
                    [range.start.line].slice(range.start.character, range.end.character);
                  return text === member || text === JSON.stringify(member);
                }),
              );
              assert.ok(JSON.stringify(hover.result).includes(member));
            } else {
              assert.equal(
                definition.result.length,
                0,
                "missing generated member retained a definition",
              );
            }
            evidence({ phase, event: "observe-complete", member, pid, definition, hover });
            reports.push({ member, present, pid, completion, definition, hover, diagnostics });
            return;
          }
          await new Promise((accept) => setImmediate(accept));
        }
      }
      for (const projection of projections) {
        await observe("initial-first", "first", true, true, projection);
        await observe("initial-missing", "addedMember", false, true, projection);
      }
      writeFileSync(input, JSON.stringify(["first", "removed", "addedMember"]));
      await refresh("add");
      for (const projection of projections)
        await observe("add", "addedMember", true, false, projection);
      await compilerSanity(false);
      writeFileSync(input, originalInput);
      await refresh("delete");
      for (const projection of projections)
        await observe("delete", "addedMember", false, true, projection);
      await compilerSanity(true);
    });
  } finally {
    writeFileSync(input, originalInput);
    writeFileSync(consumer, originalConsumer);
    evidence({ event: "restored-inputs", aborted: signal.aborted });
    if (!signal.aborted) await refresh("cleanup");
  }
});
process.stdout.write(JSON.stringify({ liveSession: true, reports }, null, 2) + "\n");
