import { expect, test } from "vitest";

import { quadruple } from "./lib.ts";

test("a relative .ts specifier resolves to the compiled sibling", () => {
  expect(quadruple(3)).toBe(12);
});
