import { expect, test } from "vitest";
import { Answer } from "native-wrapper-fixture";

test("an emitted workspace member retains its source-only npm dependency", () => {
  expect(Answer.Value).toBe(42);
});
