// ts_test's WranglerTestConfig action: a copy of a Workers config whose `main`,
// and every env.<name>.main, names the compiled entry beside the source it named.
import { copyFileSync } from "node:fs";
import { createRequire } from "node:module";
import { join, resolve } from "node:path";

const fail = (message) => {
  process.stderr.write(`wrangler_test_config: ${message}\n`);
  process.exit(1);
};

const names = { "--config": "config", "--out": "out", "--node-modules": "nodeModules" };
const flags = {};
const argv = process.argv.slice(2);
for (let i = 0; i < argv.length; i += 2) flags[names[argv[i]] ?? argv[i]] = argv[i + 1];
const { config, out, nodeModules } = flags;
if (!config || !out || !nodeModules) fail("--config, --out and --node-modules are all required");

// Resolved from the pool package's own directory, the walk the pool's
// `import("wrangler")` makes, so the copy is patched by the reader that parses it.
const require_ = createRequire(join(resolve(nodeModules), "@cloudflare", "vitest-pool-workers", "_anchor.cjs"));
let wrangler;
try {
  wrangler = require_("wrangler");
} catch (error) {
  fail(`wrangler is not in the node_modules tree (${error.message}); the pool in deps brings it`);
}
const { experimental_readRawConfig, experimental_patchConfig } = wrangler;

const COMPILED = { ".ts": ".js", ".tsx": ".js", ".mts": ".mjs", ".cts": ".cjs" };
const compiledEntry = (main) => {
  const m = /\.(mts|cts|tsx|ts)$/.exec(main);
  return m ? main.slice(0, -m[0].length) + COMPILED[m[0]] : main;
};

copyFileSync(config, out);
const { rawConfig } = experimental_readRawConfig({ config: out });
const patch = {};
if (typeof rawConfig.main === "string") patch.main = compiledEntry(rawConfig.main);
for (const [name, env] of Object.entries(rawConfig.env ?? {})) {
  if (env && typeof env.main === "string") (patch.env ??= {})[name] = { main: compiledEntry(env.main) };
}
if (Object.keys(patch).length === 0) fail(`${config} names no \`main\`, so the pool has no worker to boot`);
try {
  experimental_patchConfig(out, patch);
} catch (error) {
  fail(`cannot patch ${config}: ${error.message}`);
}
