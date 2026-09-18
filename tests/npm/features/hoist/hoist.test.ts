import assert from "node:assert/strict";
import { test } from "node:test";
import { z } from "zod";

test("a package's undeclared import resolves through the hoist", () => {
  assert.equal(z.string().parse("hoisted"), "hoisted");
});
