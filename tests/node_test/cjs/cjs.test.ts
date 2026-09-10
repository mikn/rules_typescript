import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { test } from "node:test";
import { parse } from "json5";

import { bump, dir } from "./helper";

test("runs as CommonJS at its runfiles path", () => {
  assert.equal(typeof require, "function");
  const suffix = ".runfiles/_main/tests/node_test/cjs";
  assert.ok(__dirname.endsWith(suffix), __dirname);
});

test("an extensionless relative require keeps the runfiles path", () => {
  assert.equal(bump(1), 2);
  assert.equal(dir, __dirname);
});

test("a data entry is beside __dirname", () => {
  const note = readFileSync(join(__dirname, "fixtures/note.txt"), "utf8");
  assert.equal(note.trim(), "beside the CommonJS test");
});

test("a named import from a CommonJS dependency is its export", () => {
  assert.deepEqual(parse("{a: 1}"), { a: 1 });
});
