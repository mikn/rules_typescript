import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";

test("runs under the Node .nvmrc names", () => {
  const nvmrc = readFileSync(new URL("./.nvmrc", import.meta.url), "utf8");
  console.log(`process.version ${process.version}, .nvmrc ${nvmrc.trim()}`);
  assert.equal(process.version, `v${nvmrc.trim()}`);
});
