import { expect, it } from "vitest";
import { answer } from "../foreign_fixtures/a";

it("retains transitive source and JSON imports from an excluded package", () => {
  expect(answer).toBe(42);
});
