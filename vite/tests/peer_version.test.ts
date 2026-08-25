import { createRequire } from "node:module";
import { describe, expect, it } from "vitest";

// The plugin's peerDependencies range and the Vite the ruleset builds against
// are only ever compared here, so widening one is a deliberate edit to both.
const SUPPORTED_MAJOR = 8;

// Read through the package's own exports map rather than importing "vite": Vite
// 8 ships no `types` entry, so nothing in the type graph can name the module.
const pkg = createRequire(import.meta.url)("vite/package.json") as {
  version: string;
};

describe("vite-plugin-bazel peer range", () => {
  it("runs against the Vite major it declares", () => {
    expect(Number(pkg.version.split(".")[0])).toBe(SUPPORTED_MAJOR);
  });
});
