/**
 * vite-plugin-bazel — main plugin implementation.
 *
 * Architecture
 * ────────────
 * `vite build`: Bazel pre-compiled everything to .js under bazel-bin, and the
 * plugin redirects every first-party .ts import there. Bazel owns the
 * transform; Vite only links.
 *
 * `vite dev`: Bazel is out of the inner loop. Checked-in source is handed to
 * Vite, which transforms it in memory — save-to-HMR without a Bazel analysis
 * and action cycle in between. bazel-bin is still the source of truth for what
 * Vite cannot produce: `ts_codegen` output, generated assets, and the npm tree.
 *
 * Serving source means the dev server no longer typechecks. That is native
 * parity, not a regression — Vite has never typechecked, tsserver does — but it
 * makes editor correctness load-bearing: a type error now shows up in the
 * editor and in `bazel build`, and no longer blocks the browser update.
 *
 * The hooks:
 *
 *  1. resolveId  — decide, per import, whether Vite or bazel-bin owns the file.
 *
 *  2. load       — read a pre-compiled .js from bazel-bin and attach its
 *                  .js.map. Source files are not loaded here; Vite's own
 *                  pipeline transforms them.
 *
 *  3. config     — allow serving from bazel-bin and the Bazel node_modules.
 *
 *  4. configureServer — a bazel-bin watcher, so a rebuild of generated code
 *                  reaches the browser as HMR, and a config-input watcher, so a
 *                  rebuild that changed the server's own configuration restarts
 *                  it instead of leaving it running against a stale graph.
 *
 *  5. closeBundle  — detach both watchers when the server shuts down.
 */

import fs from 'node:fs';
import path from 'node:path';
import type { Plugin, ResolvedConfig, ViteDevServer, UserConfig, ConfigEnv } from 'vite';
import { BazelResolver, type ResolverMode } from './resolver.js';
import { BazelWatcher, ConfigWatcher, bazelPathToModuleId, type ConfigInput } from './watcher.js';

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

  /** The importer's node_modules directory under bazel-bin, added to
   *  `server.fs.allow`; absolute or relative to the project root. */
  nodeModules?: string;

  /**
   * Bazel workspace name (the `name` attribute in MODULE.bazel / WORKSPACE).
   *
   * Currently unused at runtime but reserved for future runfiles-style path
   * construction.  Example: `"my_workspace"`.
   */
  workspace?: string;

  /**
   * Debounce window (ms) for aggregating ibazel rebuild events before
   * triggering HMR.
   *
   * Default: `50`.
   */
  hmrDebounceMs?: number;

  /**
   * `true` turns a watcher that fails to start into a hard error instead of a
   * warning; `false` skips the bazel-bin watcher entirely.  Unset is
   * best-effort: the dev server still boots, with a warning, and no HMR.
   */
  hmr?: boolean;

  /**
   * Inputs the generated config was produced from. A rebuild that changes one
   * of these restarts the dev server; a rebuild that changes only `ts_codegen`
   * output does not. `ts_dev_server` fills this in; a hand-written config that
   * leaves it empty gets no restart behaviour.
   */
  configInputs?: ConfigInput[];

  /**
   * Overrides the mode the resolver runs in. Normally taken from Vite itself
   * (`serve` under `vite dev`, `build` under `vite build`); set it only to pin
   * one mode in a test.
   */
  mode?: ResolverMode;
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
  let configWatcher: ConfigWatcher | null = null;
  let mode: ResolverMode = options.mode ?? 'build';

  // ── Helpers ───────────────────────────────────────────────────────────────

  /**
   * Resolve the bazel-bin path to an absolute path.
   */
  function resolveBazelBin(root: string): string {
    const raw = options.bazelBin ?? 'bazel-bin';
    return path.isAbsolute(raw) ? raw : path.resolve(root, raw);
  }

  function resolveNodeModules(root: string): string | null {
    if (options.nodeModules == null) return null;
    const nm = options.nodeModules;
    return path.isAbsolute(nm) ? nm : path.resolve(root, nm);
  }

  // ── Plugin object ─────────────────────────────────────────────────────────

  return {
    name: 'vite-plugin-bazel',
    // Enforce runs before Vite's built-in resolvers so we can intercept .ts
    // imports before Vite tries (and fails) to find them.
    enforce: 'pre',

    // ── config ──────────────────────────────────────────────────────────────
    config(userConfig: UserConfig, env: ConfigEnv): UserConfig {
      mode = options.mode ?? (env.command === 'serve' ? 'serve' : 'build');

      const root = userConfig.root != null
        ? path.resolve(userConfig.root)
        : process.cwd();

      const bazelBin = resolveBazelBin(root);
      const nodeModules = resolveNodeModules(root);

      const patch: UserConfig = {
        server: {
          fs: {
            // Vite serves the workspace root alone by default; bazel-bin and
            // the importer's node_modules join the allow list.
            allow: [
              root,
              bazelBin,
              ...(nodeModules != null ? [nodeModules] : []),
            ],
          },
          // Only with HMR off: BazelWatcher rides on server.watcher, and
          // chokidar's `ignored` would drop the path it adds there.
          ...(options.hmr === false ? { watch: { ignored: [bazelBin] } } : {}),
        },
        // Optimise dependencies from the importer's node_modules.
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
      nodeModulesAbsolute = resolveNodeModules(config.root);

      resolver = new BazelResolver({
        workspaceRoot: config.root,
        bazelBin: bazelBinAbsolute,
        workspace: options.workspace,
        mode,
      });

      config.logger.info(`[vite-plugin-bazel] bazel-bin: ${bazelBinAbsolute}`);
      config.logger.info(
        mode === 'serve'
          ? '[vite-plugin-bazel] serving first-party source; Bazel is out of the ' +
              'inner loop, so the dev server does not typecheck (tsserver and ' +
              '`bazel build` do)'
          : '[vite-plugin-bazel] serving Bazel-compiled .js from bazel-bin',
      );
      if (nodeModulesAbsolute != null) {
        config.logger.info(
          `[vite-plugin-bazel] node_modules: ${nodeModulesAbsolute}`,
        );
      }
    },

    // ── resolveId ─────────────────────────────────────────────────────────
    resolveId(id: string, importer?: string): string | null {
      const result = resolver.resolveId(id, importer);
      if (result === null) return null;

      // Either the bazel-bin .js (picked up by `load` below) or the source file
      // Vite's own pipeline transforms.
      return result.filePath;
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
    //
    // Returns nothing, and must keep returning nothing: Vite CALLS a function
    // returned from here as a post hook, so handing it a teardown closure
    // detaches both watchers before the first request, silently. Teardown lives
    // in closeBundle below.
    async configureServer(server: ViteDevServer): Promise<void> {
      await startConfigWatcher(server);

      if (options.hmr === false) return;

      watcher = new BazelWatcher({
        bazelBin: bazelBinAbsolute,
        debounceMs: options.hmrDebounceMs ?? 50,
        // Vite's own watcher: the plugin must not run a second one, and
        // chokidar is not importable outside Vite's bundle.
        source: server.watcher,
        onRebuild: (changedAbsolutePaths: Set<string>) => {
          handleRebuild(server, changedAbsolutePaths, bazelBinAbsolute);
        },
      });

      try {
        await watcher.start();
      } catch (err: unknown) {
        watcher = null;
        const detail = err instanceof Error ? err.message : String(err);
        const message =
          `[vite-plugin-bazel] no HMR: the bazel-bin watcher failed to start — ${detail}`;
        if (options.hmr === true) throw new Error(message);
        server.config.logger.warn(message);
      }
    },

    // ── closeBundle ───────────────────────────────────────────────────────
    // Vite runs this when the dev server (or a build) shuts down.
    closeBundle(): void {
      if (watcher !== null) {
        watcher.stop().catch(() => {
          // Best-effort cleanup; ignore errors during shutdown.
        });
        watcher = null;
      }
      if (configWatcher !== null) {
        configWatcher.stop().catch(() => {
          // Best-effort cleanup; ignore errors during shutdown.
        });
        configWatcher = null;
      }
    },
  };

  /**
   * Watches the inputs `ts_dev_server` generated the config from. A rebuild
   * that leaves them all identical is a codegen-only rebuild and must not
   * disturb the running server.
   */
  async function startConfigWatcher(server: ViteDevServer): Promise<void> {
    const inputs = options.configInputs ?? [];
    if (inputs.length === 0) return;

    configWatcher = new ConfigWatcher({
      inputs,
      debounceMs: options.hmrDebounceMs ?? 50,
      source: server.watcher,
      onStale: (changed: ConfigInput[]) => {
        const names = changed.map((input) => input.label).join(', ');
        server.config.logger.info(
          `[vite-plugin-bazel] restarting: ${names} changed since this server started`,
        );
        for (const input of changed) {
          if (input.remedy !== 'manual') continue;
          server.config.logger.warn(
            `[vite-plugin-bazel] ${input.label} changed, which a restart of Vite ` +
              'cannot pick up — re-run `bazel run` on this target',
          );
        }
        server.restart().catch((err: unknown) => {
          const detail = err instanceof Error ? err.message : String(err);
          server.config.logger.error(`[vite-plugin-bazel] restart failed: ${detail}`);
        });
      },
    });
    await configWatcher.start();
  }
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
