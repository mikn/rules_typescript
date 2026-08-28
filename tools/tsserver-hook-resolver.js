/**
 * tsserver-hook-resolver.js — the Bazel resolution map, shared by both consumers.
 *
 * Used by:
 *   - tools/tsserver-plugin.js, the tsserver plugin, which decorates a
 *     LanguageServiceHost. This is the path a standalone tsserver process takes.
 *   - tools/tsserver-hook.js, the `node --require` preload, for tools that call
 *     the public ts.resolveModuleName themselves.
 *
 * The map itself is built by tsserver-hook-worker.js, off-thread, out of what a
 * build already wrote down: .bazel/tsserver-hook-data.json from
 * `bazel run //:refresh_tsconfig` plus tsconfig_aspect's per-target fragments in
 * bazel-out. Nothing here runs Bazel, and nothing here needs the data to exist.
 *
 * Design constraints:
 *   - Zero npm dependencies (Node.js builtins only).
 *   - Must not crash before refresh_tsconfig has ever run.
 *   - The worker must not block the caller: until its first message arrives
 *     every lookup misses and the caller falls back to standard resolution.
 */

'use strict';

const { Worker } = require('worker_threads');
const fs = require('fs');
const path = require('path');

const ALIAS_PREFIX = '__alias__';

const ALIAS_SUFFIXES = ['.ts', '.tsx', '/index.ts', '/index.tsx', '.d.ts', '/index.d.ts'];

function debug(msg) {
  if (process.env.TSSERVER_HOOK_DEBUG) {
    process.stderr.write(`[tsserver-hook] ${msg}\n`);
  }
}

function findWorkspaceRoot(startDir) {
  let dir = startDir;
  for (;;) {
    if (fs.existsSync(path.join(dir, 'MODULE.bazel'))) return dir;
    const parent = path.dirname(dir);
    // No MODULE.bazel anywhere above: hand back where we started and let the
    // worker find nothing, rather than failing to load.
    if (parent === dir) return startDir;
    dir = parent;
  }
}

function extensionOf(fileName) {
  if (/\.d\.[mc]?ts$/.test(fileName)) return '.d.ts';
  if (fileName.endsWith('.tsx')) return '.tsx';
  if (fileName.endsWith('.mts')) return '.mts';
  if (fileName.endsWith('.cts')) return '.cts';
  return '.ts';
}

/**
 * Build a TypeScript ResolvedModuleWithFailedLookupLocations from a file path.
 *
 * @param {string} resolvedFileName - Absolute path to the resolved file.
 * @returns {{ resolvedModule: object }}
 */
function buildResolvedModule(resolvedFileName) {
  const npmPath = ['external', '.bazel'].some(
    (dir) =>
      resolvedFileName.includes(`${path.sep}${dir}${path.sep}`) ||
      resolvedFileName.includes(`/${dir}/`)
  );

  return {
    resolvedModule: {
      resolvedFileName,
      // Declarations Bazel installed or fetched are not editable workspace
      // files, and tsserver offers to rename symbols in the ones that are.
      isExternalLibraryImport: npmPath,
      extension: extensionOf(resolvedFileName),
    },
  };
}

/**
 * Start the worker for a workspace and answer module names out of its map.
 *
 * `onUpdate` runs after every map replacement, which is a caller's chance to
 * invalidate whatever it resolved from the map before.
 */
function createResolutionSource({ workspaceRoot, workerPath, onUpdate }) {
  // Key:   module name ("zod", "@acme/ui"), or "__alias__<prefix>" for a
  //        ts_path_alias prefix mapped to a source directory.
  // Value: absolute path to a .d.ts / .ts, or the directory for an alias.
  const cache = new Map();
  let ready = false;

  // TSSERVER_HOOK_PRELOAD_MAP populates the cache synchronously, so a test can
  // assert against a fixed map without racing the worker.
  if (process.env.TSSERVER_HOOK_PRELOAD_MAP) {
    try {
      for (const [key, value] of Object.entries(
        JSON.parse(process.env.TSSERVER_HOOK_PRELOAD_MAP)
      )) {
        cache.set(key, value);
      }
      ready = true;
      debug(`preloaded ${cache.size} entries from TSSERVER_HOOK_PRELOAD_MAP`);
    } catch (e) {
      debug(`failed to parse TSSERVER_HOOK_PRELOAD_MAP: ${e.message}`);
    }
  }

  const skipWorker =
    process.env.TSSERVER_HOOK_NO_WORKER === '1' ||
    process.env.TSSERVER_HOOK_NO_WORKER === 'true';

  if (!skipWorker && fs.existsSync(workerPath)) {
    try {
      const worker = new Worker(workerPath, { workerData: { workspaceRoot } });

      // unref() so a short-lived process (a test, a one-shot tool) is not held
      // open by the worker. tsserver runs indefinitely, so it changes nothing
      // there.
      worker.unref();

      worker.on('message', (msg) => {
        if (msg.type !== 'resolution-map') return;
        cache.clear();
        for (const [key, value] of Object.entries(msg.data)) {
          cache.set(key, value);
        }
        ready = true;
        debug(`resolution map ready: ${cache.size} entries`);
        if (onUpdate) {
          try {
            onUpdate();
          } catch (e) {
            debug(`onUpdate failed: ${e.message}`);
          }
        }
      });

      // Non-fatal: without a map every lookup misses and standard resolution stands.
      worker.on('error', (err) => debug(`worker error: ${err.message}`));
      worker.on('exit', (code) => {
        if (code !== 0) debug(`worker exited with code ${code}`);
      });
    } catch (e) {
      debug(`failed to spawn worker: ${e.message}`);
    }
  } else if (!skipWorker) {
    debug(`worker not found at ${workerPath} — Bazel resolution disabled`);
  }

  function lookup(moduleName) {
    if (!ready) return undefined;

    const direct = cache.get(moduleName);
    if (direct && fs.existsSync(direct)) return direct;

    for (const [key, aliasDir] of cache) {
      if (!key.startsWith(ALIAS_PREFIX)) continue;
      const prefix = key.slice(ALIAS_PREFIX.length);
      if (!moduleName.startsWith(prefix)) continue;

      const base = path.join(aliasDir, moduleName.slice(prefix.length));
      for (const suffix of ALIAS_SUFFIXES) {
        if (fs.existsSync(base + suffix)) return base + suffix;
      }
    }

    return undefined;
  }

  return {
    resolve(moduleName) {
      const hit = lookup(moduleName);
      return hit ? buildResolvedModule(hit) : undefined;
    },
  };
}

module.exports = { createResolutionSource, findWorkspaceRoot };
