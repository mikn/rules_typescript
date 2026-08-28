import { describe, it, expect } from "vitest";
import { add, multiply } from "../math";
import { thrice, twice } from "./same_package";

describe("math", () => {
  it("adds numbers", () => {
    expect(add(2, 3)).toBe(5);
  });

  it("multiplies numbers", () => {
    expect(multiply(3, 4)).toBe(12);
  });

  it("scales numbers", () => {
    expect(twice(thrice(2))).toBe(12);
  });
});
