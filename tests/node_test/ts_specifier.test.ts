import assert from "node:assert/strict";
import { test } from "node:test";

import { bump, loadedFrom } from "./counter.ts";

test("resolves a relative .ts specifier to the compiled sibling", () => {
  assert.equal(bump(1), 2);
  assert.match(loadedFrom, /\/counter\.js$/);
});
