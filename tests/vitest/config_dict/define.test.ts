import { describe, expect, it } from "vitest";

describe("inline config dict", () => {
  it("applies a vite define supplied as a dict", () => {
    expect(__RULES_TS_ANSWER__).toBe(42);
  });
});
