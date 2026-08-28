import { expect, test } from "vitest";

import { used } from "../lib/partial";

test("half of partial.ts runs", () => {
  expect(used(1)).toBe(2);
});
