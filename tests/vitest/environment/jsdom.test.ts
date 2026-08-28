import { describe, expect, it } from "vitest";

describe("jsdom environment", () => {
  it("is only ever analysed, never run", () => {
    expect(1).toBe(1);
  });
});
