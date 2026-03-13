/**
 * TanStack Start Vite plugin configuration for rules_typescript.
 *
 * This file is loaded by ts_bundle via the vite_config attr when building
 * the :spa target. It exports:
 *   - root: the package directory in the Bazel exec root, so that
 *     TanStack Start can find src/ and its entries relative to it.
 *   - plugins: tanstackStart() with router.enableRouteGeneration=false
 *     to disable the file-based route tree code generation that would
 *     otherwise attempt to write routeTree.gen.ts outside declared outputs.
 *
 * ── What tanstackStart() does in build mode ──────────────────────────────────
 *
 * tanstackStart() installs multiple Vite environments (client + server) and
 * configures them for SSR. During `vite build`, it:
 *   1. Calls @tanstack/router-generator to scan src/routes/ and optionally
 *      write routeTree.gen.ts (disabled here via enableRouteGeneration:false).
 *   2. Builds a client bundle and a server bundle via Vite builder environments.
 *   3. Runs post-build steps (manifest generation, server-function extraction).
 *
 * ── Issue 1: vite.root ───────────────────────────────────────────────────────
 *
 * rules_typescript sets vite.root = htmlDir (the HTML staging directory under
 * bazel-out) so that Rollup derives a clean HTML output filename. tanstackStart
 * resolves src/ relative to vite.root, so it would look for router.ts under
 * bazel-out/.../.../_html_staging/src/ — a path that doesn't exist.
 *
 * Fix: export `root` set to this file's directory (the package source directory
 * in the Bazel exec root, e.g. $EXEC_ROOT/examples/tanstack-app). The generated
 * Bazel vite.config.mjs uses _userRoot when present, falling back to htmlDir.
 * This makes tanstackStart resolve src/ relative to the correct directory.
 *
 * ── Issue 2: routeTree.gen.ts ────────────────────────────────────────────────
 *
 * @tanstack/router-generator scans src/routes/ and writes routeTree.gen.ts
 * back to the source tree. Bazel's sandbox does not allow writes outside
 * declared outputs — undeclared writes are silently dropped, leaving
 * routeTree.gen.ts missing and the build failing.
 *
 * Fix: router.enableRouteGeneration=false skips the generator's run() call
 * entirely. The example uses programmatic routing in src/lib/router.ts, so
 * there is no routeTree.gen.ts to generate — the manual route tree is used
 * directly.
 *
 * ── What still does NOT work with tanstackStart in Bazel ────────────────────
 *
 * tanstackStart() installs Vite builder environments (client + server) and
 * expects to produce both a client bundle AND an SSR server bundle. The Bazel
 * action declares a single output directory (spa_bundle/), but tanstackStart
 * redirects build.outDir to its own paths (.output/client, .output/server).
 * The declared outputs will not match the actual files written, causing Bazel
 * to fail with "declared output was not created".
 *
 * Additionally, the server environment build requires Node.js CJS dependencies,
 * server entry points (src/server.ts, src/client.tsx), and Nitro/H3 — none of
 * which are provided in this example.
 *
 * Bottom line: tanstackStart() requires full SSR infrastructure that is
 * fundamentally incompatible with the single-directory ts_bundle output model.
 * For a working SPA bundle, use plain Vite (no vite_config) as the :spa target
 * does by default. tanstackStart is documented here for local/pnpm use.
 *
 * ── Local (non-Bazel) use ────────────────────────────────────────────────────
 *
 * Copy this file to vite.config.mjs and run:
 *   pnpm vite build
 *
 * You will also need to provide the SSR entry points that tanstackStart expects:
 * src/server.ts, src/client.tsx — see TanStack Start docs:
 * https://tanstack.com/start/latest/docs/framework/react/quick-start
 */

import { fileURLToPath } from "node:url";
import { dirname } from "node:path";
import { tanstackStart } from "@tanstack/react-start/plugin/vite";

// Resolve the directory containing this config file. When this file is loaded
// by the generated Bazel vite.config.mjs (via dynamic import from EXEC_ROOT),
// import.meta.url points to the file's location in the exec root, e.g.:
//   file:///path/to/execroot/examples/tanstack-app/tanstack-vite.config.mjs
// dirname() gives the package root: .../execroot/examples/tanstack-app
// tanstackStart will then look for src/ relative to this directory.
// When staging_srcs is set on ts_bundle, VITE_STAGING_ROOT points at the
// writable staging directory. The framework plugin can scan route files and
// write codegen outputs there. Fall back to this file's directory for
// local (non-Bazel) use.
const root = process.env.VITE_STAGING_ROOT
  || dirname(fileURLToPath(import.meta.url));

export default {
  root,

  plugins: [
    tanstackStart({
      router: {
        // The example uses programmatic routing in src/lib/router.ts rather
        // than the default src/router.ts location. Tell tanstackStart where
        // to find it (relative to srcDirectory, which defaults to "src/").
        entry: "lib/router",

        // Route generation is ENABLED. The staging_srcs on ts_bundle copies
        // source files to a writable staging directory, so the router-generator
        // can write routeTree.gen.ts and populate the route manifest.
        // enableRouteGeneration defaults to true — no need to set it.
      },
    }),
  ],
};
