import { expect, test } from "vitest";

import { double, greeting } from "./lib";

test("double", () => {
  expect(double(21)).toBe(42);
});

test("greeting comes through the JavaScript src", () => {
  expect(greeting).toBe("hello lib");
});
