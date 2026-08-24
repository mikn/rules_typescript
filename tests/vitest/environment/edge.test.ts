import { describe, expect, it } from "vitest";

describe("edge-runtime environment", () => {
  it("is only ever analysed, never run", () => {
    expect(1).toBe(1);
  });
});
