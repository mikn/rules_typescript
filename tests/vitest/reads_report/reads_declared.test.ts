import { readFileSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, it } from "vitest";
import { workspaceRoot } from "./workspace_root.js";

describe("the same read with its files in data", () => {
  it("finds the marker and the fixture inside the runfiles", () => {
    const root = workspaceRoot(import.meta.url);
    const text = readFileSync(
      join(root, "tests/vitest/reads_report/fixtures/outside.txt"),
      "utf8",
    );
    expect(text.trim()).toBe("read from outside the test's package");
  });
});
