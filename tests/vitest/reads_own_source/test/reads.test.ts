import { existsSync, readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

import { add } from "../../math.js";
import { answer } from "../src/index.js";

const here = import.meta.dirname;

describe("a test's package at run time", () => {
  it("holds its ts_compile's source beside the compiled program", () => {
    expect(answer).toBe(42);
    const source = readFileSync(resolve(here, "../src/index.ts"), "utf8");
    expect(source.trim()).toBe("export const answer = 42;");
  });

  it("holds the test's own source at its path", () => {
    expect(existsSync(resolve(here, "reads.test.ts"))).toBe(true);
  });

  it("holds another package's program and not its source", () => {
    expect(add(1, 2)).toBe(3);
    expect(existsSync(resolve(here, "../../math.js"))).toBe(true);
    expect(existsSync(resolve(here, "../../math.ts"))).toBe(false);
  });
});
