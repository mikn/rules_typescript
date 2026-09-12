import { expect, test } from "vitest";

import { double, greeting } from "./lib";

test("double, through the declarations", () => {
  expect(double(4)).toBe(8);
});

test("greeting, through the declarations", () => {
  expect(greeting).toBe("hello lib");
});
