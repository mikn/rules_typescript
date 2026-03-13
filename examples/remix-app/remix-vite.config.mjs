/**
 * Remix Vite plugin configuration for rules_typescript.
 *
 * This file is loaded by ts_bundle via the vite_config attr. It exports:
 *   - root: the staging directory (from VITE_STAGING_ROOT) or the package
 *     source directory, so Remix can find app/ relative to it.
 *   - plugins: vitePlugin() from @remix-run/dev in SPA mode (ssr: false).
 *
 * ── What staging_srcs does for Remix ─────────────────────────────────────────
 *
 * Remix's vitePlugin() scans appDirectory (default: "app/" relative to
 * vite.root) to discover route files and generate the route manifest. It also
 * writes codegen outputs (e.g. route types) into the project.
 *
 * In the Bazel sandbox, the source tree is read-only. staging_srcs solves this
 * by:
 *   1. Copying all app/ source files into a writable _staging/ directory inside
 *      the action sandbox before Vite runs.
 *   2. Setting VITE_STAGING_ROOT to the staging dir's absolute path.
 *   3. Setting vite.root = VITE_STAGING_ROOT so Remix finds app/ there.
 *
 * This allows Remix to:
 *   - Scan app/routes/*.tsx and build the route manifest (in memory).
 *   - Write codegen files (e.g. .server/types) into the staging dir (allowed
 *     since staging is writable, unlike the source tree).
 *
 * The compiled .js outputs from ts_compile are still used via resolve.alias —
 * Bazel compiles all TypeScript, Remix just needs to discover the routes.
 *
 * ── SPA mode (ssr: false) ─────────────────────────────────────────────────────
 *
 * Remix 2.x supports a pure SPA mode via ssr: false in vitePlugin(). This:
 *   - Disables the server bundle (no Nitro/Hono server required).
 *   - Produces only a client bundle (HTML + JS/CSS/assets).
 *   - Does not require app/entry.server.tsx.
 *
 * This is the correct mode for Bazel integration since ts_bundle currently
 * produces a single output directory (not a separate server/client tree).
 *
 * ── buildDirectory — aligning Remix output with Bazel declared directory ─────
 *
 * By default, Remix's vitePlugin() overrides Vite's build.outDir and writes
 * output to "<viteRoot>/build/client/" (and "<viteRoot>/build/server/" for SSR).
 * This conflicts with the Bazel-declared output directory (app_remix_bundle/).
 *
 * We fix this by reading VITE_OUT_DIR (the Bazel-declared output path) and
 * passing it as buildDirectory to vitePlugin(). This tells Remix to write its
 * client bundle directly into app_remix_bundle/ instead of build/client/.
 *
 * In SPA mode (ssr: false), Remix writes only a "client" subdirectory, so the
 * final output is at app_remix_bundle/client/. This is a subdirectory of the
 * Bazel-declared directory, which Bazel accepts (declared_directory contains
 * all files produced by the action, including subdirectories).
 *
 * ── What still does NOT work with full SSR Remix in Bazel ─────────────────────
 *
 * Full SSR Remix (ssr: true, the default) requires:
 *   1. A server entry (app/entry.server.tsx).
 *   2. A separate server bundle build.
 *   3. A Node.js adapter (@remix-run/node or @remix-run/express).
 *
 * This is architecturally incompatible with ts_bundle's single-output model.
 * Use SPA mode for Bazel-native Remix deployments.
 *
 * ── Local (non-Bazel) use ─────────────────────────────────────────────────────
 *
 * Copy this file to vite.config.mjs and run:
 *   pnpm vite build
 *
 * Without VITE_OUT_DIR or VITE_STAGING_ROOT set, it falls back to package
 * defaults (buildDirectory = "build", appDirectory relative to packageRoot).
 */

import { fileURLToPath } from "node:url";
import { dirname } from "node:path";
import { vitePlugin as remix } from "@remix-run/dev";

// Resolve the staging root (set by the wrapper when staging_srcs is active)
// or fall back to the package source directory.
const stagingRoot = process.env["VITE_STAGING_ROOT"];
const packageRoot = dirname(fileURLToPath(import.meta.url));

// When running under Bazel, VITE_OUT_DIR is the absolute path to the declared
// output directory (app_remix_bundle/). We pass this as buildDirectory to the
// Remix plugin so its output lands inside the Bazel-declared tree.
// Without this, Remix writes to "<viteRoot>/build/" which is outside
// app_remix_bundle/ and Bazel would see the declared directory as empty.
const buildDirectory = process.env["VITE_OUT_DIR"] || undefined;

export default {
  // When staging_srcs copies app/ files into the staging dir, use that as
  // vite.root so Remix finds app/routes/*.tsx there. Fall back to the package
  // source directory for local non-Bazel use.
  root: stagingRoot || packageRoot,

  plugins: [
    remix({
      // SPA mode: disable SSR, produce only a client bundle.
      // This avoids the server infrastructure that full SSR requires.
      ssr: false,

      // Direct Remix output into the Bazel-declared directory so Bazel can
      // capture the bundle. Without this, Remix writes to build/client/ inside
      // the staging dir and Bazel sees app_remix_bundle/ as empty.
      // buildDirectory is undefined in local (non-Bazel) use, so Remix falls
      // back to its default "build/" path.
      buildDirectory,
    }),
  ],
};
