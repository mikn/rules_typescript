import { expect, it } from "vitest";
import fixture from "../foreign_json_fixture/value.json";

it("keeps a foreign JSON import at its canonical path", () => {
  const answer: number = fixture.answer;
  expect(answer).toBe(42);
});
