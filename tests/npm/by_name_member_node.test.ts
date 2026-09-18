import assert from "node:assert/strict";
import { test } from "node:test";
import { answer, twice } from "by-name-member";

test("a member's `.ts` subpath into a sibling member resolves", () => {
  assert.equal(answer, 42);
  assert.equal(twice(), 84);
});
