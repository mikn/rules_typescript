/**
 * staging_mock_plugin.mjs — Tests staging_srcs behaviour.
 *
 * This plugin simulates what a real framework plugin (e.g. @remix-run/dev,
 * tanstackStart) does when staging_srcs is set:
 *
 *   1. In configResolved, read VITE_STAGING_ROOT from the environment.
 *   2. Scan for a known route file inside the staging dir (staging_route.ts).
 *   3. Write a codegen file into the staging dir (codegen_output.txt).
 *   4. Inject a sentinel into the bundle via renderChunk to confirm the
 *      staging dir was present and readable.
 *
 * The verify_staging_srcs.sh test checks:
 *   - The bundle contains the sentinel.
 *   - VITE_STAGING_ROOT was set (sentinel encodes whether it was).
 */

import { readFileSync, writeFileSync, existsSync } from "node:fs";
import { join } from "node:path";

/** @type {import('vite').Plugin} */
const stagingMockPlugin = {
  name: "rules-typescript-staging-mock-plugin",
  configResolved() {
    const stagingRoot = process.env["VITE_STAGING_ROOT"];
    if (!stagingRoot) {
      // VITE_STAGING_ROOT not set — this is a failure sentinel.
      return;
    }
    // Try to read the staged route file.
    // The file is staged at its package-relative path, i.e. just "staging_route.ts"
    // (the tests/vite_bundle/ package prefix is stripped by rules_typescript).
    const routeFile = join(stagingRoot, "staging_route.ts");
    if (existsSync(routeFile)) {
      // Write a codegen artifact into the staging dir (simulating route tree gen).
      const codegenFile = join(stagingRoot, "codegen_output.txt");
      writeFileSync(codegenFile, "route: /staging-test\n");
    }
  },
  renderChunk(code) {
    const stagingRoot = process.env["VITE_STAGING_ROOT"] || "(not set)";
    const marker = stagingRoot !== "(not set)"
      ? "const _STAGING_ROOT_WAS_SET = true;"
      : "const _STAGING_ROOT_WAS_SET = false;";
    return marker + "\n" + code;
  },
};

export default {
  plugins: [stagingMockPlugin],
};
