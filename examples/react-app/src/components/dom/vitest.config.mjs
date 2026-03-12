/**
 * Vitest configuration for DOM tests (happy-dom environment) in the Bazel sandbox.
 *
 * Key challenges in the Bazel sandbox:
 *
 * 1. Symlink resolution: test files and compiled .js files are symlinks in the
 *    runfiles tree. Vite must NOT follow these symlinks (preserveSymlinks: true)
 *    because the resolved paths are in the execroot which is outside the sandbox.
 *
 * 2. Module resolution: vite walks UP from the importing file to find node_modules.
 *    Compiled dependency files (e.g. Button.js at _main/src/components/Button.js)
 *    import npm packages (e.g. react/jsx-runtime) but the node_modules is at
 *    _main/src/components/dom/node_modules — BELOW the importing file's level.
 *    Vite's walk-up resolution won't find it.
 *
 * Solution: a custom vite plugin that intercepts unresolved imports and tries
 * resolving them from each NODE_PATH directory.
 */

import path from "path";
import fs from "fs";

// NODE_PATH is set by the ts_test runner to point at Bazel node_modules dirs.
const nodePaths = (process.env.NODE_PATH || "").split(":").filter(Boolean);

/**
 * A vite plugin that resolves bare imports by looking in NODE_PATH directories.
 * This handles the case where compiled transitive deps import npm packages that
 * are only accessible via NODE_PATH (not via walking up from the importer).
 */
function nodePathResolverPlugin() {
  return {
    name: "bazel-node-path-resolver",
    resolveId(id, importer, options) {
      // Only handle bare imports (not relative or absolute paths).
      if (id.startsWith(".") || id.startsWith("/") || id.startsWith("\\")) {
        return null;
      }
      // Try resolving from each NODE_PATH directory.
      for (const nodeDir of nodePaths) {
        // Parse package name and sub-path.
        let pkgName, subPath;
        if (id.startsWith("@")) {
          const parts = id.split("/");
          pkgName = parts.slice(0, 2).join("/");
          subPath = parts.slice(2).join("/");
        } else {
          const slashIdx = id.indexOf("/");
          if (slashIdx === -1) {
            pkgName = id;
            subPath = "";
          } else {
            pkgName = id.slice(0, slashIdx);
            subPath = id.slice(slashIdx + 1);
          }
        }
        const pkgDir = path.join(nodeDir, pkgName);
        if (!fs.existsSync(pkgDir)) continue;
        // If there's a sub-path, try to resolve it via exports field or direct.
        if (subPath) {
          const direct = path.join(pkgDir, subPath);
          if (fs.existsSync(direct)) return direct;
          // Try with .js extension.
          if (fs.existsSync(direct + ".js")) return direct + ".js";
          // Try package.json exports field.
          const pkgJson = path.join(pkgDir, "package.json");
          if (fs.existsSync(pkgJson)) {
            try {
              const pkg = JSON.parse(fs.readFileSync(pkgJson, "utf8"));
              const exportKey = "./" + subPath;
              const exports = pkg.exports;
              if (exports && exports[exportKey]) {
                const exp = exports[exportKey];
                const resolved = typeof exp === "string" ? exp
                  : exp.default || exp.import || exp.require;
                if (resolved) {
                  const fullPath = path.join(pkgDir, resolved);
                  if (fs.existsSync(fullPath)) return fullPath;
                }
              }
            } catch (_) {}
          }
        } else {
          // No sub-path: resolve main entry.
          const pkgJson = path.join(pkgDir, "package.json");
          if (fs.existsSync(pkgJson)) {
            try {
              const pkg = JSON.parse(fs.readFileSync(pkgJson, "utf8"));
              const main = pkg.module || pkg.main || "index.js";
              const fullPath = path.join(pkgDir, main);
              if (fs.existsSync(fullPath)) return fullPath;
            } catch (_) {}
          }
          const indexJs = path.join(pkgDir, "index.js");
          if (fs.existsSync(indexJs)) return indexJs;
        }
      }
      return null;
    },
  };
}

export default {
  plugins: [nodePathResolverPlugin()],
  test: {
    environment: "happy-dom",
  },
  resolve: {
    // Do not follow symlinks to their real paths in the execroot.
    preserveSymlinks: true,
  },
};
