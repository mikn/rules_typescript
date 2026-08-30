#!/usr/bin/env bash
# ts_codegen generator for the TanStack Router route tree.
#
# Takes --out <file> and --srcs "<route> <route> ...", and reads NODE_BINARY and
# TS_CODEGEN_NODE_MODULES, all of which ts_codegen sets.
#
# --start-router <path> makes it a TanStack START tree rather than a plain
# Router one: the framework's own Vite plugin appends a `declare module` footer
# naming the router entry, and regenerates the tree in dev. Without the same
# footer here, running the dev server rewrites the checked-in file and the
# staleness test that guards it goes red. <path> is the router entry as the tree
# file imports it, e.g. ../lib/router.ts
set -euo pipefail

node="${NODE_BINARY:-node}"
out=""
srcs=""
start_router=""
while [ $# -gt 0 ]; do
  case "$1" in
    --out) out="$2"; shift 2 ;;
    --srcs) srcs="$2"; shift 2 ;;
    --start-router) start_router="$2"; shift 2 ;;
    *) echo "tanstack_routes: unexpected argument '$1'" >&2; exit 1 ;;
  esac
done

if [ -z "$out" ] || [ -z "$srcs" ]; then
  echo "tanstack_routes: --out and --srcs are both required" >&2
  exit 1
fi
if [ -z "${TS_CODEGEN_NODE_MODULES:-}" ]; then
  echo "tanstack_routes: TS_CODEGEN_NODE_MODULES is unset — run this through ts_codegen with node_modules set" >&2
  exit 1
fi

# read -a, not an unquoted expansion: {srcs} arrives as one whitespace-separated
# argument, and a route file named with a glob character must survive the split.
read -r -a files <<< "$srcs"
routes_root=""
for f in "${files[@]}"; do
  d="$(dirname "$f")"
  if [ -z "$routes_root" ] || [ "${#d}" -lt "${#routes_root}" ]; then
    routes_root="$d"
  fi
done

# The generator writes each route's import path relative to the tree file, so
# tree and routes have to sit in one directory -- the layout of src/routes.
# Under the output dir, not $TMPDIR: the generator finishes with a rename() from
# a scratch dir it puts in the execroot, which fails across devices.
work="$(mktemp -d -p "$(dirname "$out")" .route_tree.XXXXXX)"
trap 'rm -rf "$work"' EXIT
for f in "${files[@]}"; do
  rel="${f#"$routes_root"/}"
  mkdir -p "$work/routes/$(dirname "$rel")"
  cp "$f" "$work/routes/$rel"
done

cat > "$work/generate.mjs" <<'MJS'
import { createRequire } from "node:module";
import { resolve } from "node:path";

const [routesDir, treeFile, startRouter] = process.argv.slice(2);
const require_ = createRequire(resolve(process.env.TS_CODEGEN_NODE_MODULES) + "/_anchor.cjs");
const { Generator, getConfig } = require_("@tanstack/router-generator");

const routesDirectory = resolve(routesDir);
const config = await getConfig(
  {
    routesDirectory,
    generatedRouteTree: resolve(treeFile),
    target: "react",
    routeFileIgnorePattern: "routeTree\\.gen\\.ts",
    disableLogging: true,
    // Byte-for-byte what @tanstack/start-plugin-core's moduleDeclaration()
    // appends, so the dev server regenerating this file is a no-op.
    ...(startRouter
      ? {
          routeTreeFileFooter: [
            [
              `import type { getRouter } from '${startRouter}'`,
              "import type { createStart } from '@tanstack/react-start'",
              "declare module '@tanstack/react-start' {",
              "  interface Register {",
              "    ssr: true",
              "    router: Awaited<ReturnType<typeof getRouter>>",
              "  }",
              "}",
            ].join("\n"),
          ],
        }
      : {}),
  },
  // configDirectory: keeps a stray tsr.config.json outside src/routes out of it.
  routesDirectory,
);
await new Generator({ config, root: routesDirectory }).run();
MJS

"$node" "$work/generate.mjs" "$work/routes" "$work/routes/routeTree.gen.ts" "$start_router"
cp "$work/routes/routeTree.gen.ts" "$out"
