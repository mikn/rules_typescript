/**
 * Compiles one .svelte component for svelte_library.
 *
 *   node svelte_compile.mjs \
 *     --node-modules <dir> --generate client|server --dev true|false \
 *     --filename <workspace-relative name> --src <path> \
 *     --js-out <path> --map-out <path> --css-out <path>
 */

import { createRequire } from "node:module";
import { readFileSync, writeFileSync } from "node:fs";
import { basename, resolve } from "node:path";

function parseArgs(argv) {
  const out = {};
  for (let i = 0; i < argv.length; i += 2) {
    if (!argv[i].startsWith("--") || i + 1 >= argv.length) {
      throw new Error(`svelte_compile: cannot parse arguments at ${argv[i]}`);
    }
    out[argv[i].slice(2)] = argv[i + 1];
  }
  return out;
}

function loadCompiler(nodeModulesDir) {
  // The 'svelte/compiler' require condition is a single self-contained CommonJS
  // bundle, so createRequire resolves it without the parent-directory walk an
  // ESM import of the package's own sources would need.
  const anchor = resolve(nodeModulesDir, "svelte_library_resolution_anchor.cjs");
  try {
    return createRequire(anchor)("svelte/compiler");
  } catch (err) {
    throw new Error(
      `svelte_library: cannot load 'svelte/compiler' from ${nodeModulesDir}\n` +
        `Add "@npm//:svelte" to the deps of the node_modules() target this ` +
        `svelte_library names.\n${err.message}`,
    );
  }
}

function describe(err) {
  const at = err.start ? `:${err.start.line}:${err.start.column}` : "";
  const code = err.code ? `[${err.code}] ` : "";
  return `${err.filename ?? "<unknown>"}${at}: ${code}${err.message}`;
}

function main() {
  const args = parseArgs(process.argv.slice(2));
  const { compile } = loadCompiler(args["node-modules"]);
  const source = readFileSync(args.src, "utf8");

  let compiled;
  try {
    compiled = compile(source, {
      filename: args.filename,
      generate: args.generate,
      dev: args.dev === "true",
    });
  } catch (err) {
    process.stderr.write(`${describe(err)}\n`);
    process.exit(1);
  }

  for (const warning of compiled.warnings) {
    process.stderr.write(`warning: ${describe(warning)}\n`);
  }

  const sourceMappingURL = `\n//# sourceMappingURL=${basename(args["map-out"])}\n`;
  writeFileSync(args["js-out"], compiled.js.code + sourceMappingURL);
  writeFileSync(args["map-out"], JSON.stringify(compiled.js.map));

  // A component with no <style> block gets an empty file: Starlark cannot see
  // the block, so the output was declared either way and has to exist.
  writeFileSync(args["css-out"], compiled.css ? compiled.css.code : "");
}

try {
  main();
} catch (err) {
  process.stderr.write(`${err.message}\n`);
  process.exit(1);
}
