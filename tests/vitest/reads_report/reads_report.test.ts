import { readFileSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, it } from "vitest";
import { workspaceRoot } from "./workspace_root.js";

describe("a read the runfiles do not hold", () => {
  it("reaches the fixture through the workspace root", () => {
    const root = workspaceRoot(import.meta.url);
    const text = readFileSync(
      join(root, "tests/vitest/reads_report/fixtures/outside.txt"),
      "utf8",
    );
    expect(text.trim()).toBe("read from outside the test's package");
  });
});
