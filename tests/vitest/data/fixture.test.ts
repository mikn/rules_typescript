import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";

describe("data attr", () => {
  it("finds a fixture listed in data", () => {
    const root = process.env.TEST_SRCDIR ?? ".";
    const text = readFileSync(`${root}/_main/tests/vitest/data/fixture.txt`, "utf8");
    expect(text.trim()).toBe("the fixture was in runfiles");
  });
});
