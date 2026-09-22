import { expect, test } from "vitest";
import { Answer } from "./lib/library";

test("Vitest transforms an enum from a source-only dependency", () => {
  expect(Answer.Value).toBe(42);
});
