import { createRequire } from "node:module";
import { expect, test } from "vitest";

const require = createRequire(import.meta.url);
const versionBeside = (from: string, name: string): string =>
  createRequire(from)(`${name}/package.json`).version;

test("each dependent resolves its own edge", () => {
  expect(require("minimatch/package.json").version).toBe("9.0.9");
  const testExclude = require.resolve("test-exclude/package.json");
  expect(versionBeside(testExclude, "minimatch")).toBe("10.2.4");
});
