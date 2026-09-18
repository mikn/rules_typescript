// Wrangler writes beside its config, so generation needs a writable copy of the source layout.

import { copyFileSync, mkdirSync, mkdtempSync, rmSync, symlinkSync } from "node:fs";
import { spawnSync } from "node:child_process";
import { createRequire } from "node:module";
import { basename, dirname, join, relative, resolve } from "node:path";

const fail = (message) => {
  process.stderr.write(`wrangler_types: ${message}\n`);
  process.exit(1);
};

const names = { "--config": "config", "--out": "out", "--srcs": "srcs" };
const flags = { config: "", out: "", srcs: "" };
const argv = process.argv.slice(2);
let next = 0;
for (; next < argv.length && names[argv[next]]; next += 2) {
  flags[names[argv[next]]] = argv[next + 1] ?? "";
}
const wranglerArgs = argv.slice(next);

const { config, out, srcs } = flags;
if (!config || !out || !srcs) fail("--config, --out and --srcs are all required");
const nodeModules = process.env.TS_CODEGEN_NODE_MODULES;
if (!nodeModules) {
  fail("TS_CODEGEN_NODE_MODULES is unset — run this through ts_codegen with node_modules set");
}
const wrangler = join(resolve(nodeModules), "wrangler", "bin", "wrangler.js");

const files = srcs.split(/\s+/).filter(Boolean);
const configFile = files.find((f) => basename(f) === config);
if (!configFile) {
  fail(`'${config}' is not among srcs:\n  ${files.join("\n  ")}`);
}
const configDir = dirname(configFile);

const work = mkdtempSync(join(dirname(out), ".worker_types."));
try {
  for (const file of files) {
    const rel = relative(configDir, file);
    if (rel.startsWith("..")) {
      fail(
        `'${file}' is not under '${configDir}', the directory holding ${config}.\n` +
          "wrangler resolves `main` relative to the config, so a src outside that " +
          "directory has no staged path wrangler could name.",
      );
    }
    const staged = join(work, rel);
    mkdirSync(dirname(staged), { recursive: true });
    copyFileSync(file, staged);
  }

  // `wrangler types` runs the config's build.command before it resolves `main`
  // and drops the entry when the command fails, so the copy has no `build`.
  const require_ = createRequire(resolve(nodeModules) + "/_anchor.cjs");
  const { experimental_readRawConfig, experimental_patchConfig } = require_("wrangler");
  const stagedConfig = join(work, config);
  const { rawConfig } = experimental_readRawConfig({ config: stagedConfig });
  const patch = {};
  if (rawConfig.build) patch.build = undefined;
  for (const [name, env] of Object.entries(rawConfig.env ?? {})) {
    if (env?.build) (patch.env ??= {})[name] = { build: undefined };
  }
  if (Object.keys(patch).length > 0) {
    try {
      experimental_patchConfig(stagedConfig, patch);
    } catch (error) {
      fail(
        `cannot remove build from ${config}: ${error.message}\n` +
          "wrangler refuses to patch a .toml containing '#'; " +
          "move the config to wrangler.jsonc.",
      );
    }
  }

  // wrangler resolves from the staged config by walking up, and its own
  // imports from its realpath in the store, to the links beside its tree.
  symlinkSync(resolve(nodeModules), join(work, "node_modules"));

  // Wrangler includes argv in generated headers; omit redundant flags to match hand runs.
  const discovered = ["wrangler.jsonc", "wrangler.json", "wrangler.toml"].includes(config);
  const home = join(work, ".home");
  mkdirSync(home, { recursive: true });
  const child = spawnSync(
    process.execPath,
    [wrangler, "types", ...(discovered ? [] : ["-c", config]), ...wranglerArgs],
    {
      cwd: work,
      stdio: ["ignore", "pipe", "inherit"],
      env: {
        ...process.env,
        HOME: home,
        XDG_CONFIG_HOME: home,
        CI: "true",
        WRANGLER_SEND_METRICS: "false",
        NODE_PATH: resolve(nodeModules),
      },
    },
  );
  if (child.error) fail(`${wrangler}: ${child.error.message}`);
  if (child.status !== 0) {
    process.stderr.write(child.stdout?.toString() ?? "");
    fail(`wrangler types exited ${child.status}`);
  }
  copyFileSync(join(work, "worker-configuration.d.ts"), out);
} finally {
  rmSync(work, { recursive: true, force: true });
}
