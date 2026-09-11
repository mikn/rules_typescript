import assert from "node:assert/strict";
import { test } from "node:test";

import { loadedFrom, twice } from "self-reference";

test("the package's own name resolves to its compiled file", () => {
  assert.equal(twice(2), 4);
  assert.match(loadedFrom, /\/self_reference\/src\/index\.js$/);
});
