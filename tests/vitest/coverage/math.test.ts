import { describe, it, expect } from "vitest";
import { add, multiply } from "../math";

describe("math", () => {
  it("adds numbers", () => {
    expect(add(2, 3)).toBe(5);
  });

  it("multiplies numbers", () => {
    expect(multiply(3, 4)).toBe(12);
  });
});
