import assert from "node:assert/strict";
import { mkdtempSync, readFileSync, writeFileSync } from "node:fs";
import { join, resolve } from "node:path";
import { spawnSync } from "node:child_process";

const [patcher, nodeModules] = process.argv.slice(2);
const temp = mkdtempSync(join(process.env.TEST_TMPDIR, "entries-"));
writeFileSync(join(temp, "entry.ts"), "export const incidental = true;\n");
let count = 0;
function project(main, sources, javascript) {
  const config = join(temp, `config-${count++}.json`);
  const out = config + ".out.json";
  const original = JSON.stringify({ name: "runtime-entry", main });
  writeFileSync(config, original);
  const result = spawnSync(
    process.execPath,
    [
      patcher,
      "--config",
      config,
      "--config-path",
      "../fixture/worker/wrangler.json",
      "--out",
      out,
      "--node-modules",
      resolve(nodeModules),
      ...sources.flatMap((file) => ["--runtime-source", file]),
      ...javascript.flatMap((file) => ["--runtime-js", file]),
    ],
    { encoding: "utf8" },
  );
  assert.equal(readFileSync(config, "utf8"), original);
  return {
    ...result,
    main: result.status === 0 ? JSON.parse(readFileSync(out, "utf8")).main : undefined,
  };
}
for (const [source, emitted] of [
  ["ts", "js"],
  ["tsx", "js"],
  ["mts", "mjs"],
  ["cts", "cjs"],
]) {
  const main = `./src/../entry.${source}`;
  const src = `../fixture/worker/entry.${source}`;
  const js = `../fixture/worker/entry.${emitted}`;
  assert.equal(project(main, [src], []).main, main);
  assert.equal(project(main, [], [js]).main, `./src/../entry.${emitted}`);
  const ambiguous = project(main, [src], [js]);
  assert.notEqual(ambiguous.status, 0);
  assert.match(ambiguous.stderr, /both source and emitted runtime owners/);
}
for (const extension of ["js", "mjs", "cjs"]) {
  const main = `entry.${extension}`;
  assert.equal(project(main, [], [`../fixture/worker/${main}`]).main, main);
}
assert.equal(project("missing.ts", [], []).main, "missing.ts");
console.log(
  "Runtime owner conflicts reject; source, emitted and unmatched paths preserve their declared identities.",
);
