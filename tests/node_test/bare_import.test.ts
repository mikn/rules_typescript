import assert from "node:assert/strict";
import { test } from "node:test";
import { z } from "zod";

test("a bare specifier resolves through the importer into the store", () => {
  assert.ok(z.string().safeParse("x").success);
  const store =
    /\/tests\/npm\/node_modules\/\.pnpm\/zod@3\.24\.2\/node_modules\/zod\//;
  assert.match(import.meta.resolve("zod"), store);
});
