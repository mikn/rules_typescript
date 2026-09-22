import assert from "node:assert/strict";
import { test } from "node:test";

test("registers with node:test", () => {
  assert.equal(1 + 1, 2);
});

test("and a second one, so a zero-collect run is unmistakable", () => {
  assert.ok(true);
});
