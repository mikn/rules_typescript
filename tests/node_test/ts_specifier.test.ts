import assert from "node:assert/strict";
import { test } from "node:test";

import { bump } from "./counter.ts";

test("resolves a relative .ts specifier at runtime", () => {
  assert.equal(bump(1), 2);
});
