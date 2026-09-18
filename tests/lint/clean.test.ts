import { expect, test } from "vitest";

import { add } from "./clean";

test("adds", () => {
  expect(add(1, 2)).toBe(3);
});
