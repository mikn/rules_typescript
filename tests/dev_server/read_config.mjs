/**
 * read_config.mjs — evaluate a generated vite.config.mjs and print what it says.
 *
 *   node read_config.mjs <vite.config.mjs>
 *
 * The config is a real module that reads its environment, so the only honest way
 * to ask what port (or root, or allow-list) it configures is to run it.
 */

import path from 'node:path';

const configPath = process.argv[2];
const config = (await import(path.resolve(configPath))).default;

process.stdout.write(
  JSON.stringify({
    port: config?.server?.port ?? null,
    host: config?.server?.host ?? null,
    root: config?.root ?? null,
    fsAllow: config?.server?.fs?.allow ?? null,
    watchPaths: config?.server?.watch?.paths ?? null,
  })
);
