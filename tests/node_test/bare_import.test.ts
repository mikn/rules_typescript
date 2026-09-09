import assert from "node:assert/strict";
import { test } from "node:test";
import { z } from "zod";

test("a bare specifier resolves from the test's node_modules tree", () => {
  assert.ok(z.string().safeParse("x").success);
  const tree = /\/bare_import_test\/node_modules\/zod\//;
  assert.match(import.meta.resolve("zod"), tree);
});
