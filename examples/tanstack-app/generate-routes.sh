#!/usr/bin/env bash
# TanStack Router route tree generator for Bazel ts_codegen.
#
# This script is invoked by the ts_codegen rule as a Bazel build action.
# It writes the Node.js generator code to a temp file and runs it via $NODE_BINARY.
#
# Environment variables set by ts_codegen:
#   NODE_BINARY             — path to Node.js (from js_runtime toolchain)
#   NODE_PATH               — node_modules directory (for CJS require resolution)
#   TS_CODEGEN_NODE_MODULES — same as NODE_PATH (for explicit require paths)
#
# Arguments (from ts_codegen args):
#   --routes-dir <dir>   — execroot-relative path to the routes directory
#   --out <file>         — execroot-relative path for the generated output file
set -euo pipefail

# Resolve Node.js binary. ts_codegen sets NODE_BINARY from the js_runtime
# toolchain; fall back to plain `node` if not set.
NODE="${NODE_BINARY:-node}"

# Write the generator script to a temp file and run it.
# We use a heredoc to avoid needing a separate .mjs file as a build input.
# The script uses CJS require() via createRequire so it can load
# @tanstack/router-generator from NODE_PATH set by ts_codegen.
TMPSCRIPT="$(mktemp /tmp/tanstack-route-gen.XXXXXX.mjs)"
trap 'rm -f "$TMPSCRIPT"' EXIT

cat > "$TMPSCRIPT" << 'SCRIPT_END'
import { createRequire } from 'node:module';
import { resolve } from 'node:path';
import { parseArgs } from 'node:util';

const { values } = parseArgs({
  options: {
    'routes-dir': { type: 'string' },
    'out': { type: 'string' },
  },
  strict: true,
});

const routesDir = values['routes-dir'];
const outFile = values['out'];

if (!routesDir || !outFile) {
  process.stderr.write('generate-routes: --routes-dir and --out are required\n');
  process.exit(1);
}

// Resolve all paths to absolute (Bazel build actions run from the execroot).
const absoluteRoutesDir = resolve(routesDir);
const absoluteOut = resolve(outFile);

// Load @tanstack/router-generator from the node_modules tree.
// ts_codegen sets TS_CODEGEN_NODE_MODULES to the execroot-relative
// node_modules directory. We resolve it to an absolute path for createRequire.
const nodeModulesDir = process.env.TS_CODEGEN_NODE_MODULES;
if (!nodeModulesDir) {
  process.stderr.write(
    'generate-routes: TS_CODEGEN_NODE_MODULES is not set.\n' +
    'This script must be run via ts_codegen with node_modules set.\n'
  );
  process.exit(1);
}

// createRequire requires an absolute path. We anchor the require scope to a
// dummy file path inside the node_modules directory so that CJS resolution
// finds @tanstack/router-generator from that location.
const absoluteNmDir = resolve(nodeModulesDir);
const req = createRequire(absoluteNmDir + '/_anchor.cjs');

let Generator, getConfig;
try {
  const pkg = req('@tanstack/router-generator');
  Generator = pkg.Generator;
  getConfig = pkg.getConfig;
} catch (err) {
  process.stderr.write(
    `generate-routes: Failed to load @tanstack/router-generator:\n${err.message}\n`
  );
  process.exit(1);
}

(async () => {
  try {
    const config = await getConfig(
      {
        routesDirectory: absoluteRoutesDir,
        generatedRouteTree: absoluteOut,
        disableLogging: true,
      },
      // configDirectory: where to look for tsr.config.json. Set to routes
      // dir to avoid reading stray config from the Bazel sandbox CWD.
      absoluteRoutesDir,
    );

    const generator = new Generator({ config, root: absoluteRoutesDir });

    // run() scans the routes directory and writes the route tree file.
    await generator.run();

    process.stdout.write(`TsCodegen: generated ${absoluteOut}\n`);
  } catch (err) {
    process.stderr.write(
      `generate-routes: Error during generation:\n${err.stack || err.message}\n`
    );
    process.exit(1);
  }
})();
SCRIPT_END

exec "$NODE" "$TMPSCRIPT" "$@"
