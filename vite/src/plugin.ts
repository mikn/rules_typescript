/**
 * vite-plugin-bazel — main plugin implementation.
 *
 * Architecture
 * ────────────
 * Bazel (via ibazel) pre-compiles all TypeScript to .js under bazel-bin/.
 * This plugin's job is:
 *
 *  1. resolveId  — intercept imports of .ts source files (and implicit .ts
 *                  extension-less imports) and redirect them to their
 *                  pre-compiled .js counterparts in bazel-bin.
 *
 *  2. load       — read the pre-compiled .js from bazel-bin and attach the
 *                  .js.map source map when it exists, so that browser devtools
 *                  show the original TypeScript.
 *
 *  3. config     — configure Vite to allow serving files from bazel-bin and
 *                  to resolve npm modules from the Bazel-generated node_modules
 *                  tree.
 *
 *  4. configureServer — install a BazelWatcher on bazel-bin so that when
 *                  ibazel finishes a rebuild the changed modules are invalidated
 *                  in Vite's module graph and the browser receives an HMR
 *                  update.
 *
 * No transforms happen inside this plugin.  The JS that Bazel produced is
 * served verbatim; Vite never re-compiles TypeScript.
 */

import fs from 'node:fs';
import path from 'node:path';
import type { Plugin, ResolvedConfig, ViteDevServer, UserConfig, ConfigEnv } from 'vite';
import { BazelResolver } from './resolver.js';
import { BazelWatcher, bazelPathToModuleId } from './watcher.js';

// ---------------------------------------------------------------------------
// Public types
// ---------------------------------------------------------------------------

export interface BazelPluginOptions {
  /**
   * Path to the bazel-bin output tree, relative to the Vite project root
   * (or absolute).
   *
   * Default: `"bazel-bin"`.
   */
  bazelBin?: string;

  /**
   * Absolute (or root-relative) path to the generated node_modules directory
   * that Bazel produces via the `node_modules` rule.
   *
   * When omitted the plugin attempts to auto-detect it by looking for a
   * directory named `<target_name>_node_modules` inside bazel-bin.
   *
   * If neither this option nor `target` is set, npm resolution falls back to
   * Vite's default behaviour (the project-root `node_modules`).
   */
  nodeModules?: string;

  /**
   * Bazel workspace name (the `name` attribute in MODULE.bazel / WORKSPACE).
   *
   * Currently unused at runtime but reserved for future runfiles-style path
   * construction.  Example: `"my_workspace"`.
   */
  workspace?: string;

  /**
   * Bazel target label for the dev server, e.g. `"//app:dev"`.
   *
   * Used to derive the default `nodeModules` path when `nodeModules` is not
   * explicitly provided: the plugin looks for
   * `bazel-bin/<package>/<name>_node_modules`.
   */
  target?: string;

  /**
   * Debounce window (ms) for aggregating ibazel rebuild events before
   * triggering HMR.
   *
   * Default: `50`.
   */
  hmrDebounceMs?: number;
}

// ---------------------------------------------------------------------------
// Plugin factory
// ---------------------------------------------------------------------------

export function bazelPlugin(options: BazelPluginOptions = {}): Plugin {
  // ── State ────────────────────────────────────────────────────────────────
  // These are set in configResolved (guaranteed to run before resolveId /
  // load / configureServer).  Definite-assignment assertions reflect that.
  let bazelBinAbsolute!: string;
  let nodeModulesAbsolute: string | null = null;
  let resolver!: BazelResolver;
  let watcher: BazelWatcher | null = null;

  // ── Helpers ───────────────────────────────────────────────────────────────

  /**
   * Resolve the bazel-bin path to an absolute path.
   */
  function resolveBazelBin(root: string): string {
    const raw = options.bazelBin ?? 'bazel-bin';
    return path.isAbsolute(raw) ? raw : path.resolve(root, raw);
  }

  /**
   * Attempt to auto-detect the generated node_modules directory.
   *
   * Resolution order:
   *  1. Explicit `options.nodeModules` (absolute or root-relative).
   *  2. Derived from `options.target`: `bazel-bin/<pkg>/<name>_node_modules`.
   *  3. `bazel-bin/<workspace>_node_modules` (legacy single-workspace layout).
   *  4. null — fall through to Vite's default node_modules resolution.
   */
  function resolveNodeModules(root: string, bazelBin: string): string | null {
    if (options.nodeModules != null) {
      const nm = options.nodeModules;
      return path.isAbsolute(nm) ? nm : path.resolve(root, nm);
    }

    if (options.target != null) {
      const derived = nodeModulesFromTarget(options.target, bazelBin);
      if (derived != null && fs.existsSync(derived)) return derived;
    }

    // Fallback: scan bazel-bin for any *_node_modules directory at the top
    // level (handles single-package workspaces without an explicit target).
    if (fs.existsSync(bazelBin)) {
      try {
        const entries = fs.readdirSync(bazelBin, { withFileTypes: true });
        for (const entry of entries) {
          if (entry.isDirectory() && entry.name.endsWith('_node_modules')) {
            return path.join(bazelBin, entry.name);
          }
        }
      } catch {
        // Ignore — bazel-bin may not exist yet (first run before any build).
      }
    }

    return null;
  }

  // ── Plugin object ─────────────────────────────────────────────────────────

  return {
    name: 'vite-plugin-bazel',
    // Enforce runs before Vite's built-in resolvers so we can intercept .ts
    // imports before Vite tries (and fails) to find them.
    enforce: 'pre',

    // ── config ──────────────────────────────────────────────────────────────
    config(userConfig: UserConfig, _env: ConfigEnv): UserConfig {
      const root = userConfig.root != null
        ? path.resolve(userConfig.root)
        : process.cwd();

      const bazelBin = resolveBazelBin(root);
      const nodeModules = resolveNodeModules(root, bazelBin);

      const patch: UserConfig = {
        resolve: {
          // When a generated node_modules tree is available, prepend it to
          // Node's module resolution search path so that `import "react"` finds
          // the Bazel-managed package rather than whatever is in the project
          // root's node_modules.
          ...(nodeModules != null
            ? { modules: [nodeModules, 'node_modules'] }
            : {}),
        },
        server: {
          fs: {
            // Allow Vite's dev server to serve files from bazel-bin (and the
            // generated node_modules) — by default Vite restricts serving to
            // the workspace root.
            allow: [
              root,
              bazelBin,
              ...(nodeModules != null ? [nodeModules] : []),
            ],
          },
          watch: {
            // Exclude bazel-bin from Vite's own watcher — we manage that
            // separately via BazelWatcher so we can debounce ibazel bursts.
            ignored: [bazelBin],
          },
        },
        // Optimise dependencies from the generated node_modules.
        optimizeDeps: {
          ...(nodeModules != null
            ? { include: [], exclude: [] }
            : {}),
        },
      };

      return patch;
    },

    // ── configResolved ────────────────────────────────────────────────────
    configResolved(config: ResolvedConfig): void {
      bazelBinAbsolute = resolveBazelBin(config.root);
      nodeModulesAbsolute = resolveNodeModules(config.root, bazelBinAbsolute);

      resolver = new BazelResolver({
        workspaceRoot: config.root,
        bazelBin: bazelBinAbsolute,
        workspace: options.workspace,
      });

      config.logger.info(
        `[vite-plugin-bazel] bazel-bin: ${bazelBinAbsolute}`,
        { once: true },
      );
      if (nodeModulesAbsolute != null) {
        config.logger.info(
          `[vite-plugin-bazel] node_modules: ${nodeModulesAbsolute}`,
          { once: true },
        );
      }
    },

    // ── resolveId ─────────────────────────────────────────────────────────
    resolveId(id: string, importer?: string): string | null {
      const result = resolver.resolveId(id, importer);
      if (result === null) return null;

      // Return the absolute path to the .js file in bazel-bin.  Vite will
      // call `load` with this ID on its next pass.
      return result.jsPath;
    },

    // ── load ──────────────────────────────────────────────────────────────
    load(id: string): { code: string; map?: string | null } | null {
      // Only handle files that live under bazel-bin.
      if (!id.startsWith(bazelBinAbsolute + path.sep) && id !== bazelBinAbsolute) {
        return null;
      }
      // Only handle .js files — let Vite's default loader handle everything else.
      if (!id.endsWith('.js')) return null;

      let code: string;
      try {
        code = fs.readFileSync(id, 'utf8');
      } catch {
        // File doesn't exist yet (build hasn't run for this target).
        return null;
      }

      // Locate the companion .js.map file.
      const mapPath = resolver.findMapForJs(id);
      let map: string | null = null;
      if (mapPath !== null) {
        try {
          map = fs.readFileSync(mapPath, 'utf8');
        } catch {
          // Map file disappeared between the existence check and the read;
          // continue without it.
        }
      }

      return { code, map };
    },

    // ── configureServer ───────────────────────────────────────────────────
    configureServer(server: ViteDevServer): (() => void) | void {
      // Start the BazelWatcher after the server has started.  We return a
      // cleanup function that Vite calls when the server is torn down.

      const hmrDebounceMs = options.hmrDebounceMs ?? 50;

      watcher = new BazelWatcher({
        bazelBin: bazelBinAbsolute,
        debounceMs: hmrDebounceMs,
        onRebuild: (changedAbsolutePaths: Set<string>) => {
          handleRebuild(server, changedAbsolutePaths, bazelBinAbsolute);
        },
      });

      // Start is async; fire and forget from within the synchronous plugin
      // hook.  Any errors are caught and logged so they don't crash the server.
      watcher.start().catch((err: unknown) => {
        server.config.logger.error(
          `[vite-plugin-bazel] failed to start bazel-bin watcher: ${String(err)}`,
        );
      });

      return function cleanup() {
        if (watcher !== null) {
          watcher.stop().catch(() => {
            // Best-effort cleanup; ignore errors during shutdown.
          });
          watcher = null;
        }
      };
    },
  };
}

// ---------------------------------------------------------------------------
// HMR: handle a completed ibazel rebuild
// ---------------------------------------------------------------------------

/**
 * Called after the debounce window expires with the set of .js files that
 * changed in bazel-bin.
 *
 * Strategy:
 *  1. For each changed .js path, compute its Vite module ID.
 *  2. Look up the module in Vite's module graph.
 *  3. Invalidate any matching modules so Vite knows they are stale.
 *  4. Send an HMR update to the browser.
 *
 * If a changed module has no HMR boundary in its import chain Vite will
 * trigger a full-page reload.  This is the correct safe fallback.
 */
function handleRebuild(
  server: ViteDevServer,
  changedAbsolutePaths: Set<string>,
  bazelBin: string,
): void {
  const modulesToUpdate: string[] = [];

  for (const absPath of changedAbsolutePaths) {
    const moduleId = bazelPathToModuleId(absPath, bazelBin);
    if (moduleId === null) continue;

    // Try both the absolute path and the module-ID form, because Vite can
    // store modules under either key depending on how they were first loaded.
    const candidates = [absPath, moduleId];

    let found = false;
    for (const key of candidates) {
      const mods = server.moduleGraph.getModulesByFile(key);
      if (mods != null && mods.size > 0) {
        for (const mod of mods) {
          server.moduleGraph.invalidateModule(mod);
        }
        found = true;
      }
    }

    if (found) {
      modulesToUpdate.push(absPath);
    } else {
      // Module not in the graph yet — it was probably loaded but not yet
      // registered (e.g. a new file from a new target).  Invalidate by
      // absolute path anyway; Vite will pick it up on the next request.
      server.moduleGraph.invalidateAll();
      // A full reload is the safest option when we can't find the module.
      server.ws.send({ type: 'full-reload' });
      return;
    }
  }

  if (modulesToUpdate.length === 0) return;

  // Send HMR updates for all invalidated modules in a single batch.
  server.ws.send({
    type: 'update',
    updates: modulesToUpdate.map((absPath) => ({
      type: 'js-update' as const,
      path: bazelPathToModuleId(absPath, bazelBin) ?? absPath,
      acceptedPath: bazelPathToModuleId(absPath, bazelBin) ?? absPath,
      timestamp: Date.now(),
      explicitImportRequired: false,
      isWithinCircularImport: false,
    })),
  });
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/**
 * Derives the expected node_modules path from a Bazel target label.
 *
 * Label format: `//package/path:target_name`
 *   → `bazel-bin/package/path/target_name_node_modules`
 *
 * Returns null when the label cannot be parsed.
 */
function nodeModulesFromTarget(target: string, bazelBin: string): string | null {
  // Strip leading `//`.
  const withoutSlashes = target.startsWith('//') ? target.slice(2) : target;
  const colonIdx = withoutSlashes.indexOf(':');
  if (colonIdx === -1) return null;

  const pkg = withoutSlashes.slice(0, colonIdx);    // e.g. "app"
  const name = withoutSlashes.slice(colonIdx + 1);   // e.g. "dev"

  return path.join(bazelBin, pkg, `${name}_node_modules`);
}
