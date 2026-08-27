import { fileURLToPath } from "node:url";
import { dirname, resolve } from "node:path";
import { tanstackStart } from "@tanstack/react-start/plugin/vite";

// ts_bundle stages the sources into a writable directory and points
// VITE_STAGING_ROOT at it, which is also the vite root it sets. The fallback is
// for running this config outside Bazel.
const root =
  process.env["VITE_STAGING_ROOT"] || dirname(fileURLToPath(import.meta.url));

// TanStack Start bakes each route file's ABSOLUTE path into the route-manifest
// module, and under Bazel that is the per-action sandbox path: without this the
// chunk's content hash, and so its filename, changes on every build.
function stableRoutePaths(from) {
  const prefix = resolve(from) + "/";
  return {
    name: "bazel-stable-route-paths",
    enforce: "post",
    transform(code, id) {
      if (!id.includes("tanstack-start-manifest") || !code.includes(prefix)) {
        return null;
      }
      return { code: code.split(prefix).join(""), map: null };
    },
  };
}

export default {
  plugins: [
    tanstackStart({
      router: {
        entry: "lib/router",
        generatedRouteTree: "routes/routeTree.gen.ts",
        routeFileIgnorePattern: "routeTree\\.gen\\.ts",
      },
    }),
    stableRoutePaths(root),
  ],
};
