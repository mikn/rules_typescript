import {describe, expect, it} from "vitest";

describe("a staged ambient declaration", () => {
  it("puts a global in the program the test files are in", () => {
    // The global is declared and never defined, so `typeof` is the one
    // reference to it that both type-checks and runs.
    const origin: typeof STAGED_ORIGIN = "origin";
    expect(origin).toBe("origin");
    expect(typeof STAGED_ORIGIN).toBe("undefined");
  });
});
