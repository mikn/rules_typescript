/**
 * tsserver-hook.js — Bazel-aware tsserver resolution hook for rules_typescript.
 *
 * The TypeScript equivalent of GOPACKAGESDRIVER.
 *
 * Load this script via:
 *   node --require ./tools/tsserver-hook.js
 *
 * When loaded, this script:
 *   1. Walks up from cwd to find the workspace root (directory containing
 *      MODULE.bazel).
 *   2. Spawns a background worker thread that runs `bazel query` and scans
 *      the @npm external repo to build a module-name → .d.ts path map.
 *   3. Monkey-patches ts.resolveModuleName so that imports like
 *      `import { z } from "zod"` resolve to the .d.ts in the Bazel output
 *      base instead of relying on paths in tsconfig.json.
 *   4. Falls back to the standard TypeScript resolver for anything not in the
 *      Bazel resolution map.
 *
 * Design constraints:
 *   - Zero npm dependencies (Node.js builtins only).
 *   - Must not crash if Bazel is not installed or packages are not fetched.
 *   - Worker thread must not block the main thread (tsserver).
 *   - Cache is rebuilt automatically when BUILD files or pnpm-lock.yaml change.
 */

'use strict';

const { Worker } = require('worker_threads');
const path = require('path');
const fs = require('fs');
const Module = require('module');

// ── Locate workspace root ─────────────────────────────────────────────────────
// Walk up from cwd looking for MODULE.bazel (bzlmod marker).
let workspaceRoot = process.cwd();
for (;;) {
  if (fs.existsSync(path.join(workspaceRoot, 'MODULE.bazel'))) {
    break;
  }
  const parent = path.dirname(workspaceRoot);
  if (parent === workspaceRoot) {
    // Reached filesystem root without finding MODULE.bazel — leave
    // workspaceRoot as cwd and let the worker degrade gracefully.
    workspaceRoot = process.cwd();
    break;
  }
  workspaceRoot = parent;
}

// ── Resolution cache ──────────────────────────────────────────────────────────
// Populated by the worker thread via postMessage.
// Key:   module name (e.g. "zod", "vitest", "@/components/Button")
// Value: absolute path to .d.ts or .ts source file
const resolutionCache = new Map();
let cacheReady = false;

// ── Synchronous pre-load (test / CI use) ──────────────────────────────────────
// When TSSERVER_HOOK_PRELOAD_MAP is set to a JSON string (an object mapping
// module names to .d.ts paths), the cache is populated synchronously before
// the worker thread is even spawned.  This lets tests inject a deterministic
// resolution map without waiting for the async worker.
//
// Example:
//   TSSERVER_HOOK_PRELOAD_MAP='{"zod":"/path/to/zod.d.ts"}' \
//     node --require ./tools/tsserver-hook.js resolve_test.mjs
if (process.env.TSSERVER_HOOK_PRELOAD_MAP) {
  try {
    const preload = JSON.parse(process.env.TSSERVER_HOOK_PRELOAD_MAP);
    for (const [key, value] of Object.entries(preload)) {
      resolutionCache.set(key, value);
    }
    cacheReady = true;
    if (process.env.TSSERVER_HOOK_DEBUG) {
      process.stderr.write(
        `[tsserver-hook] preloaded ${resolutionCache.size} entries from TSSERVER_HOOK_PRELOAD_MAP\n`
      );
    }
  } catch (e) {
    if (process.env.TSSERVER_HOOK_DEBUG) {
      process.stderr.write(
        `[tsserver-hook] failed to parse TSSERVER_HOOK_PRELOAD_MAP: ${e.message}\n`
      );
    }
  }
}

// ── Spawn background worker ───────────────────────────────────────────────────
// The worker is co-located next to this file so that consumers can install
// both files together.
//
// TSSERVER_HOOK_NO_WORKER=1 skips worker spawning entirely.  This is useful
// when the cache is pre-populated via TSSERVER_HOOK_PRELOAD_MAP and the caller
// does not want a background thread (e.g. short-lived test processes).
const workerPath = path.join(__dirname, 'tsserver-hook-worker.js');

let worker = null;

const skipWorker =
  process.env.TSSERVER_HOOK_NO_WORKER === '1' ||
  process.env.TSSERVER_HOOK_NO_WORKER === 'true';

if (!skipWorker && fs.existsSync(workerPath)) {
  try {
    worker = new Worker(workerPath, {
      workerData: { workspaceRoot },
    });

    // unref() so the worker thread does not prevent the process from exiting
    // once the main thread's work is done.  tsserver itself runs indefinitely,
    // so this has no effect in normal use; it matters for short-lived test
    // processes where we don't want to wait for the worker to finish.
    worker.unref();

    worker.on('message', (msg) => {
      if (msg.type === 'resolution-map') {
        resolutionCache.clear();
        const entries = Object.entries(msg.data);
        for (const [key, value] of entries) {
          resolutionCache.set(key, value);
        }
        cacheReady = true;
        if (process.env.TSSERVER_HOOK_DEBUG) {
          process.stderr.write(
            `[tsserver-hook] resolution map ready: ${entries.length} entries\n`
          );
        }
      }
    });

    worker.on('error', (err) => {
      if (process.env.TSSERVER_HOOK_DEBUG) {
        process.stderr.write(`[tsserver-hook] worker error: ${err.message}\n`);
      }
      // Non-fatal: let the hook continue without Bazel resolution.
    });

    worker.on('exit', (code) => {
      if (code !== 0 && process.env.TSSERVER_HOOK_DEBUG) {
        process.stderr.write(`[tsserver-hook] worker exited with code ${code}\n`);
      }
    });
  } catch (e) {
    if (process.env.TSSERVER_HOOK_DEBUG) {
      process.stderr.write(`[tsserver-hook] failed to spawn worker: ${e.message}\n`);
    }
  }
} else if (!skipWorker && process.env.TSSERVER_HOOK_DEBUG) {
  process.stderr.write(
    `[tsserver-hook] worker not found at ${workerPath} — hook disabled\n`
  );
}

// ── Monkey-patch TypeScript ───────────────────────────────────────────────────
// TypeScript is loaded AFTER our --require script runs (it is loaded by
// tsserver, not by us).  We intercept Module._load so we can patch the
// TypeScript module as soon as it is first required.
//
// Modern TypeScript (>=5.0) ships its exports as non-configurable, getter-only
// properties (Object.defineProperty with only a `get`, no `set`).  Direct
// assignment (ts.resolveModuleName = ...) therefore throws a TypeError.
//
// The fix is to:
//  1. Capture the original ts.resolveModuleName reference before any patch.
//  2. Replace the cached module exports with a Proxy that intercepts property
//     access, returning our Bazel-aware wrapper for resolveModuleName.
//  3. Methods other than resolveModuleName are returned as-is (bound to the
//     real ts object so `this` is correct inside them).

let tsPatched = false;

const originalLoad = Module._load;

Module._load = function bazelHookLoad(request, parent, isMain) {
  const result = originalLoad.apply(this, arguments);

  // Match both "typescript" and paths ending in "/typescript" (e.g. when
  // tsserver loads its bundled copy from a node_modules subdirectory).
  if (
    !tsPatched &&
    (request === 'typescript' ||
      request.endsWith('/typescript') ||
      request.endsWith(path.sep + 'typescript'))
  ) {
    if (result && typeof result.resolveModuleName === 'function') {
      return patchTypeScript(result);
    }
  }

  return result;
};

/**
 * Wrap the TypeScript module with a Proxy that intercepts resolveModuleName.
 *
 * TypeScript >=5.0 defines exports as non-configurable getter properties, so
 * we cannot use direct assignment.  A Proxy is the only reliable approach.
 *
 * The wrapper:
 *   1. Consults resolutionCache (populated by the worker) for direct hits.
 *   2. Handles path-alias prefixes stored as "__alias__<prefix>" keys.
 *   3. Falls back to the original resolver for anything not in the cache.
 *
 * @param {object} ts - The original TypeScript module object.
 * @returns {Proxy} A proxy that forwards everything to ts but overrides
 *                  resolveModuleName and exposes _bazelPatched = true.
 */
function patchTypeScript(ts) {
  tsPatched = true;

  const originalResolve = ts.resolveModuleName;

  function bazelResolveModuleName(
    moduleName,
    containingFile,
    compilerOptions,
    host,
    cache,
    redirectedReference,
    resolutionMode
  ) {
    if (cacheReady) {
      // ── Direct cache hit ────────────────────────────────────────────────────
      if (resolutionCache.has(moduleName)) {
        const resolved = resolutionCache.get(moduleName);
        if (resolved && fs.existsSync(resolved)) {
          return buildResolvedModule(resolved);
        }
      }

      // ── Path-alias resolution ───────────────────────────────────────────────
      // Keys like "__alias__@/" map an alias prefix to a source directory.
      // We try each registered alias prefix in order.
      for (const [key, aliasDir] of resolutionCache.entries()) {
        if (!key.startsWith('__alias__')) continue;

        const aliasPrefix = key.slice('__alias__'.length); // e.g. "@/"
        if (!moduleName.startsWith(aliasPrefix)) continue;

        const rest = moduleName.slice(aliasPrefix.length); // e.g. "components/Button"
        const base = path.join(aliasDir, rest);

        // Try common extensions / index variants.
        for (const suffix of [
          '.ts',
          '.tsx',
          '/index.ts',
          '/index.tsx',
          '.d.ts',
          '/index.d.ts',
        ]) {
          const candidate = base + suffix;
          if (fs.existsSync(candidate)) {
            return buildResolvedModule(candidate);
          }
        }
      }
    }

    // ── Fallback: standard TypeScript resolver ──────────────────────────────
    return originalResolve.call(ts, moduleName, containingFile, compilerOptions, host, cache, redirectedReference, resolutionMode);
  }

  // Build a Proxy that transparently forwards all property accesses to the
  // real ts module, except:
  //   - resolveModuleName → our Bazel-aware wrapper
  //   - _bazelPatched     → true (sentinel for tests and self-check)
  const proxy = new Proxy(ts, {
    get(target, prop, receiver) {
      if (prop === 'resolveModuleName') return bazelResolveModuleName;
      if (prop === '_bazelPatched') return true;
      const value = target[prop];
      // Bind functions to the real target so internal `this` references work.
      if (typeof value === 'function') return value.bind(target);
      return value;
    },
  });

  // Replace the module in Node's require cache so that any subsequent
  // require('typescript') call gets the proxy, not the original object.
  // This is important when tsserver requires typescript in multiple places.
  try {
    const resolvedPath = require.resolve('typescript');
    if (Module._cache[resolvedPath]) {
      Module._cache[resolvedPath].exports = proxy;
    }
  } catch (_) {
    // require.resolve might fail in unusual setups; non-fatal.
  }

  if (process.env.TSSERVER_HOOK_DEBUG) {
    process.stderr.write('[tsserver-hook] patched ts.resolveModuleName (Proxy)\n');
  }

  return proxy;
}

/**
 * Build a TypeScript ResolvedModuleWithFailedLookupLocations from a file path.
 *
 * @param {string} resolvedFileName - Absolute path to the resolved file.
 * @returns {{ resolvedModule: object }}
 */
function buildResolvedModule(resolvedFileName) {
  let extension;
  if (resolvedFileName.endsWith('.d.ts') || resolvedFileName.endsWith('.d.mts') || resolvedFileName.endsWith('.d.cts')) {
    extension = '.d.ts';
  } else if (resolvedFileName.endsWith('.tsx')) {
    extension = '.tsx';
  } else if (resolvedFileName.endsWith('.mts')) {
    extension = '.mts';
  } else if (resolvedFileName.endsWith('.cts')) {
    extension = '.cts';
  } else {
    extension = '.ts';
  }

  return {
    resolvedModule: {
      resolvedFileName,
      // Mark packages in bazel's external/ directory as external library
      // imports so tsserver doesn't treat them as editable workspace files.
      isExternalLibraryImport:
        resolvedFileName.includes(`${path.sep}external${path.sep}`) ||
        resolvedFileName.includes('/external/'),
      extension,
    },
  };
}
