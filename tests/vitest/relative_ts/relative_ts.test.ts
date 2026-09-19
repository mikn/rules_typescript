import { expect, test } from "vitest";

import { libUrl, quadruple, utilUrl } from "./lib.ts";

test("a relative .ts specifier runs the compiled sibling", () => {
  expect(quadruple(3)).toBe(12);
  expect(libUrl).toMatch(/\/lib\.js$/);
  expect(utilUrl).toMatch(/\/deep\/util\.js$/);
});
