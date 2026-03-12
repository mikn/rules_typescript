import { describe, expect, it } from "vitest";

import { add, divide, multiply, subtract } from "./math";

describe("add", () => {
  it("returns the sum of two positive numbers", () => {
    expect(add(1, 2)).toBe(3);
  });

  it("handles negative operands", () => {
    expect(add(-4, 4)).toBe(0);
  });
});

describe("multiply", () => {
  it("returns the product of two numbers", () => {
    expect(multiply(3, 4)).toBe(12);
  });

  it("returns zero when either operand is zero", () => {
    expect(multiply(0, 99)).toBe(0);
  });
});

describe("subtract", () => {
  it("returns the difference", () => {
    expect(subtract(10, 3)).toBe(7);
  });
});

describe("divide", () => {
  it("returns the quotient", () => {
    expect(divide(12, 4)).toBe(3);
  });

  it("throws RangeError when dividing by zero", () => {
    expect(() => divide(1, 0)).toThrow(RangeError);
  });
});
