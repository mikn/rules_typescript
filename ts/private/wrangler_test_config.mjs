import { copyFileSync, existsSync, realpathSync } from "node:fs";
import { createRequire } from "node:module";
import { join, posix, resolve } from "node:path";

const fail = (message) => {
  process.stderr.write(`wrangler_test_config: ${message}\n`);
  process.exit(1);
};

const names = {
  "--config": "config",
  "--out": "out",
  "--config-path": "configPath",
  "--node-modules": "nodeModules",
  "--runtime-source": "runtimeSources",
  "--runtime-js": "runtimeJs",
};
const flags = { nodeModules: [], runtimeSources: [], runtimeJs: [] };
const argv = process.argv.slice(2);
for (let i = 0; i < argv.length; i += 2) {
  const name = names[argv[i]] ?? argv[i];
  if (Array.isArray(flags[name])) flags[name].push(argv[i + 1]);
  else flags[name] = argv[i + 1];
}
const { config, out, configPath, nodeModules } = flags;
if (!config || !out || !configPath || nodeModules.length === 0) {
  fail("--config, --out, --config-path and --node-modules are all required");
}

// Resolved from the pool package's realpath in the store, the walk the pool's
// `import("wrangler")` makes, so the copy is patched by the reader that parses it.
const pool = nodeModules
  .map((dir) => join(resolve(dir), "@cloudflare", "vitest-pool-workers"))
  .find((dir) => existsSync(dir));
if (!pool) {
  const dirs = nodeModules.join(", ");
  fail(`@cloudflare/vitest-pool-workers is linked by none of ${dirs}`);
}
const require_ = createRequire(join(realpathSync(pool), "_anchor.cjs"));
let wrangler;
try {
  wrangler = require_("wrangler");
} catch (error) {
  fail(`wrangler is not beside the pool's tree: ${error.message}`);
}
const { experimental_readRawConfig, experimental_patchConfig } = wrangler;

const COMPILED = { ".ts": ".js", ".tsx": ".js", ".mts": ".mjs", ".cts": ".cjs" };
const compiledEntry = (main) => {
  const m = /\.(mts|cts|tsx|ts)$/.exec(main);
  return m ? main.slice(0, -m[0].length) + COMPILED[m[0]] : main;
};

const sources = new Set(flags.runtimeSources);
const javascript = new Set(flags.runtimeJs);
const runtimeEntry = (main) => {
  const source = posix.normalize(posix.join(posix.dirname(configPath), main));
  const emitted = compiledEntry(source);
  const ownsSource = sources.has(source);
  const ownsEmitted = javascript.has(emitted);
  if (ownsSource && ownsEmitted && source !== emitted) {
    fail(
      `${configPath}: ${main} has both source and emitted runtime owners; use one runtime identity in the test dependencies`,
    );
  }
  return ownsEmitted && !ownsSource ? compiledEntry(main) : main;
};

copyFileSync(config, out);
const { rawConfig } = experimental_readRawConfig({ config: out });
const patch = {};
if (typeof rawConfig.main === "string") patch.main = runtimeEntry(rawConfig.main);
for (const [name, env] of Object.entries(rawConfig.env ?? {})) {
  if (env && typeof env.main === "string")
    (patch.env ??= {})[name] = { main: runtimeEntry(env.main) };
}
if (Object.keys(patch).length === 0)
  fail(`${config} names no \`main\`, so the pool has no worker to boot`);
try {
  experimental_patchConfig(out, patch);
} catch (error) {
  fail(`cannot patch ${config}: ${error.message}`);
}
