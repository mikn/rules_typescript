import assert from "node:assert/strict";
import { mock, test } from "node:test";

test("mock.module replaces a relative import", async () => {
  mock.module("./mocked.ts", { namedExports: { value: "mocked" } });
  const { value } = await import("./mocked.ts");
  assert.equal(value, "mocked");
});
