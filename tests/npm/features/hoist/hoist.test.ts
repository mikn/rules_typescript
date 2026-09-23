import assert from "node:assert/strict";
import { createRequire } from "node:module";
import { test } from "node:test";
import { z } from "zod";

test("a package's undeclared import resolves through the hoist", () => {
  assert.equal(z.string().parse("hoisted"), "hoisted");
});

test("workspace YAML public hoist is unavailable to first-party source", () => {
  const { customAlphabet } = createRequire(import.meta.url)("nano-alias");
  assert.equal(customAlphabet("x", 3)(), "xxx");
});
