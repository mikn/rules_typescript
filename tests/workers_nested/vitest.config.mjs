import { cloudflareTest } from "@cloudflare/vitest-pool-workers";
import { realpathSync } from "node:fs";
import { dirname, resolve } from "node:path";

// A build output the runfiles do not hold and every build writes to disk (a
// FileWrite, never a cache-hit spawn's): the worker's ownership record.
const stagedConfig = realpathSync(resolve(import.meta.dirname, "wrangler.jsonc"));
const undeclared = resolve(dirname(stagedConfig), "../worker.ownership");

// Written as a worker's own config is: configPath relative to the worker root,
// an environment, and no `resolve` key.
export default {
  plugins: [
    cloudflareTest({
      wrangler: { configPath: "./wrangler.jsonc", environment: process.env.WORKERS_TEST_ENV ?? "test" },
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
