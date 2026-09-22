import assert from "node:assert/strict";
import { test } from "node:test";

import { bump, loadedFrom } from "./counter";
import { greet } from "./nested";

test("an extensionless relative specifier loads the compiled sibling", () => {
  assert.equal(bump(1), 2);
  assert.match(loadedFrom, /\/counter\.js$/);
});

test("a directory specifier loads its compiled index", () => {
  assert.equal(greet(), "nested");
});
