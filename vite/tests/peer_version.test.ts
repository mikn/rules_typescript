import { readFileSync } from "node:fs";
import { createRequire } from "node:module";
import { describe, expect, it } from "vitest";

// The plugin's declared peer range and the Vite the ruleset builds against are
// in two different files, so widening one is a deliberate edit to both.
const manifest = JSON.parse(
  readFileSync(
    `${process.env.TEST_SRCDIR ?? process.env.RUNFILES_DIR ?? "."}/_main/vite/package.json`,
    "utf8",
  ),
) as { peerDependencies: { vite: string } };

// Read through the package's own exports map rather than importing "vite": Vite
// 8 ships no `types` entry, so nothing in the type graph can name the module.
const pkg = createRequire(import.meta.url)("vite/package.json") as {
  version: string;
};

describe("vite-plugin-bazel peer range", () => {
  it("runs against a Vite major it declares", () => {
    const majors = manifest.peerDependencies.vite
      .split("||")
      .map((range) => Number(range.trim().replace(/^\D+/, "").split(".")[0]));
    expect(majors).toContain(Number(pkg.version.split(".")[0]));
  });
});
