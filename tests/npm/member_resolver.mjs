import { readFileSync, writeFileSync } from "node:fs";
import { createRequire } from "node:module";
import { join, resolve } from "node:path";

const args = process.argv.slice(2);
const specifiers = readFileSync(args[args.indexOf("--specifiers") + 1], "utf8")
  .split("\n")
  .filter(Boolean);
const out = args[args.indexOf("--out") + 1];

const nodeModules = resolve(process.env.TS_CODEGEN_NODE_MODULES);
// createRequire takes a filename, not a file: the walk up starts beside it.
const require = createRequire(join(nodeModules, "..", "generator.cjs"));
const marker = "node_modules/";

const lines = specifiers.map((specifier) => {
  const resolved = require.resolve(specifier);
  const inStore = resolved.slice(resolved.lastIndexOf(marker) + marker.length);
  const name = specifier.replace(/\W/g, "_");
  return `export const ${name}: string = ${JSON.stringify(inStore)};`;
});
writeFileSync(out, lines.join("\n") + "\n");
