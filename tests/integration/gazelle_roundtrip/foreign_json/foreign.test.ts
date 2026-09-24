import { expect, it } from "vitest";
import fixture from "../foreign_fixtures/value.json";

it("projects an excluded package JSON import into compiler and runtime inputs", () => {
  const answer: number = fixture.answer;
  expect(answer).toBe(42);
});
