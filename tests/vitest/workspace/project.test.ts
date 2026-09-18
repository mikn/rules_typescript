import { describe, expect, it } from "vitest";

describe("vitest workspace project", () => {
  it("runs inside a named project through the merged include", () => {
    expect(expect.getState().testPath).toMatch(/\/project\.test\.js$/);
  });
});
