import { cloudflareTest } from "@cloudflare/vitest-pool-workers";
import { realpathSync } from "node:fs";
import { dirname, resolve } from "node:path";

// The realpath of a build output the runfiles do not hold: the worker's .d.ts,
// beside the compiled test in bazel-out.
const compiledTest = realpathSync(
  resolve(import.meta.dirname, "test/worker.test.js"),
);
const undeclared = resolve(dirname(compiledTest), "../src/index.d.ts");

// Written as a worker's own config is: configPath relative to the worker root,
// an environment, and no `resolve` key.
export default {
  plugins: [
    cloudflareTest({
      wrangler: { configPath: "./wrangler.jsonc", environment: "test" },
    }),
    {
      name: "undeclared",
      config: () => ({
        define: { __UNDECLARED__: JSON.stringify(undeclared) },
      }),
    },
  ],
  test: {
    coverage: { provider: "istanbul", include: ["src/**"] },
  },
};
