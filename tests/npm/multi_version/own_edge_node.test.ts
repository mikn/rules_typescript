import assert from "node:assert/strict";
import { createRequire } from "node:module";
import test from "node:test";

const require = createRequire(import.meta.url);
const versionBeside = (from: string, name: string): string =>
  createRequire(from)(`${name}/package.json`).version;

test("each dependent resolves its own edge", () => {
  assert.equal(require("minimatch/package.json").version, "9.0.9");
  const testExclude = require.resolve("test-exclude/package.json");
  assert.equal(versionBeside(testExclude, "minimatch"), "10.2.4");
});
