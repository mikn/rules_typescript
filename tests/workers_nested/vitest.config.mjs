import { cloudflareTest } from "@cloudflare/vitest-pool-workers";

// Written as a worker's own config is: configPath relative to the worker root,
// an environment, and no `resolve` key.
export default {
  plugins: [
    cloudflareTest({
      wrangler: { configPath: "./wrangler.jsonc", environment: "test" },
    }),
  ],
  test: {
    coverage: { provider: "istanbul", include: ["src/**"] },
  },
};
