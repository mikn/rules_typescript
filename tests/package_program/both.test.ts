import { expect, test } from "vitest";

import { double } from "./lib";
import { greeting, quadruple } from "./mid";

test("quadruple, through mid's declarations", () => {
  expect(quadruple(3)).toBe(12);
});

test("greeting through mid, double through lib's source", () => {
  expect(greeting).toBe("hello lib");
  expect(double(2)).toBe(4);
});
