import assert from "node:assert/strict";
import { test } from "node:test";

import type { Point } from "@shared/value";

test("type-checks against the aliased declaration", () => {
  const p: Point = { x: 1, y: 2 };
  assert.equal(p.x + p.y, 3);
});

test("sees the global the staged declaration puts in the program", () => {
  // Declared and never defined, so `typeof` is the one reference that both
  // type-checks and runs.
  const origin: typeof STAGED_ORIGIN = "origin";
  assert.equal(origin, "origin");
  assert.equal(typeof STAGED_ORIGIN, "undefined");
});
