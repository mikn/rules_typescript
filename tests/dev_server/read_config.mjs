/**
 * read_config.mjs — evaluate a generated vite.config.mjs and print what it says.
 *
 *   node read_config.mjs <vite.config.mjs>
 *
 * The config is a real module that reads its environment and the filesystem, so
 * the only honest way to ask what port (or alias, or watched input) it
 * configures is to run it.
 */

import path from 'node:path';

const configPath = process.argv[2];
const module = await import(path.resolve(configPath));
const config = module.default;

process.stdout.write(
  JSON.stringify({
    port: config?.server?.port ?? null,
    host: config?.server?.host ?? null,
    root: config?.root ?? null,
    fsAllow: config?.server?.fs?.allow ?? null,
    watchPaths: config?.server?.watch?.paths ?? null,
    // A `find` is a RegExp for an exact match and a string for the subpath
    // prefix; both go over the wire as text.
    alias: (config?.resolve?.alias ?? []).map((entry) => ({
      find: String(entry.find),
      replacement: entry.replacement,
    })),
    configInputs: module.bazelConfigInputs ?? null,
    // Vite flattens nested plugin arrays (one npm plugin package can be
    // several), and so does this, so the order is the order Vite sees.
    plugins: (config?.plugins ?? [])
      .flat(Infinity)
      .filter((plugin) => plugin && plugin.name)
      .map((plugin) => plugin.name),
    // Not a Vite option in any version -- webpack's. Reported so a test can
    // fail if it comes back.
    resolveModules: config?.resolve?.modules ?? null,
  })
);
