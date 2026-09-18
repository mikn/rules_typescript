import { expect, it } from "vitest";

it("runs the compiled file", () => {
  expect(import.meta.url.endsWith(".test.js")).toBe(true);
});
