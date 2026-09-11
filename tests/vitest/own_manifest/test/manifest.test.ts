import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

import { answer } from "own-manifest";

const pkg = JSON.parse(
  readFileSync(resolve(import.meta.dirname, "../package.json"), "utf8"),
) as { name: string; exports: Record<string, string> };

describe("the package's manifest at run time", () => {
  it("is the src as written", () => {
    expect(pkg.name).toBe("own-manifest");
    expect(pkg.exports["."]).toBe("./src/index.ts");
  });

  it("answers the package's own name", () => {
    expect(answer).toBe(42);
  });
});
