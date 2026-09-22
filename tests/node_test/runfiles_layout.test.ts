import assert from "node:assert/strict";
import { existsSync, readFileSync } from "node:fs";
import { test } from "node:test";

test("runs at its runfiles path", () => {
  const suffix = ".runfiles/_main/tests/node_test/runfiles_layout.test.js";
  assert.ok(import.meta.url.endsWith(suffix), import.meta.url);
});

test("has its own source staged beside it", () => {
  assert.ok(existsSync(new URL("./runfiles_layout.test.ts", import.meta.url)));
});

test("has a data entry beside it", () => {
  const url = new URL("./fixtures/note.txt", import.meta.url);
  const note = readFileSync(url, "utf8");
  assert.equal(note.trim(), "the data entry was beside the test");
});
