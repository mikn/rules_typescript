import { describe, it, expect } from "vitest";
import { greet, formatCurrency } from "./index";

describe("greet", () => {
  it("returns greeting with name", () => {
    expect(greet("World")).toBe("Hello, World! Built with rules_typescript.");
  });

  it("returns greeting with different name", () => {
    expect(greet("Bazel")).toBe("Hello, Bazel! Built with rules_typescript.");
  });
});

describe("formatCurrency", () => {
  it("formats USD by default", () => {
    expect(formatCurrency(1234.56)).toBe("$1,234.56");
  });

  it("formats other currencies", () => {
    expect(formatCurrency(99.99, "EUR")).toBe("€99.99");
  });
});
