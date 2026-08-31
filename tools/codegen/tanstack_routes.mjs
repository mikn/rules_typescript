// ts_codegen generator for the TanStack Router route tree.
//
// Takes --out <file> and --srcs "<route> <route> ...", and reads
// TS_CODEGEN_NODE_MODULES, all of which ts_codegen sets.
//
// --start-router <path> makes it a TanStack START tree rather than a plain
// Router one: the framework's own Vite plugin appends a `declare module` footer
// naming the router entry, and regenerates the tree in dev. Without the same
// footer here, running the dev server rewrites the checked-in file and the
// staleness test that guards it goes red. <path> is the router entry as the
// tree file imports it, e.g. ../lib/router.ts

import { copyFileSync, mkdirSync, mkdtempSync, rmSync } from "node:fs";
import { createRequire } from "node:module";
import { dirname, join, relative, resolve } from "node:path";

const fail = (message) => {
  process.stderr.write(`tanstack_routes: ${message}\n`);
  process.exit(1);
};

const names = { "--out": "out", "--srcs": "srcs", "--start-router": "startRouter" };
const flags = { out: "", srcs: "", startRouter: "" };
const argv = process.argv.slice(2);
for (let i = 0; i < argv.length; i += 2) {
  const key = names[argv[i]];
  if (!key) fail(`unexpected argument '${argv[i]}'`);
  flags[key] = argv[i + 1] ?? "";
}

const { out, srcs, startRouter } = flags;
if (!out || !srcs) fail("--out and --srcs are both required");
const nodeModules = process.env.TS_CODEGEN_NODE_MODULES;
if (!nodeModules) {
  fail("TS_CODEGEN_NODE_MODULES is unset — run this through ts_codegen with node_modules set");
}

const files = srcs.split(/\s+/).filter(Boolean);
const routesRoot = files
  .map(dirname)
  .reduce((shortest, d) => (d.length < shortest.length ? d : shortest));

// Under the output dir, not the system temp dir: the generator finishes with a
// rename() out of a scratch dir, which fails across devices.
const work = mkdtempSync(join(dirname(out), ".route_tree."));
try {
  // The generator writes each route's import path relative to the tree file, so
  // tree and routes have to sit in one directory -- the layout of src/routes.
  const routesDirectory = resolve(work, "routes");
  for (const file of files) {
    const staged = join(routesDirectory, relative(routesRoot, file));
    mkdirSync(dirname(staged), { recursive: true });
    copyFileSync(file, staged);
  }

  const require_ = createRequire(resolve(nodeModules) + "/_anchor.cjs");
  const { Generator, getConfig } = require_("@tanstack/router-generator");

  const generatedRouteTree = join(routesDirectory, "routeTree.gen.ts");
  const config = await getConfig(
    {
      routesDirectory,
      generatedRouteTree,
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
  copyFileSync(generatedRouteTree, out);
} finally {
  rmSync(work, { recursive: true, force: true });
}
