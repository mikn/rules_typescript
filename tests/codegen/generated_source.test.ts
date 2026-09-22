import { expect, test } from "vitest";
import { GENERATED } from "./generated";

test("a generated TypeScript source runs without declaration or JavaScript emission", () => {
  expect(GENERATED).toBe(true);
});
