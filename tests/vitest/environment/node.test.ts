import { describe, expect, it } from "vitest";

describe("node environment", () => {
  it("has no DOM, so the attr is what decides", () => {
    expect(typeof document).toBe("undefined");
    expect(typeof window).toBe("undefined");
  });
});
