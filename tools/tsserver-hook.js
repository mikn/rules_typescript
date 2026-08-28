/**
 * tsserver-hook.js — Bazel-aware resolution for tools that call ts.resolveModuleName.
 *
 * Load this script via:
 *   node --require ./tools/tsserver-hook.js
 *
 * It intercepts Module._load and replaces the `typescript` module's
 * resolveModuleName with one that consults the Bazel resolution map first
 * (tsserver-hook-resolver.js), falling back to TypeScript's own resolver.
 *
 * **This does not reach a standalone tsserver process, and is not meant to.**
 * tsserver's language service resolves through its LanguageServiceHost, not
 * through the public export patched here, so an editor that spawns tsserver --
 * VS Code, coc-tsserver, typescript-language-server, lsp-mode -- needs
 * tsserver-plugin.js instead. What is left for this file is the other kind of
 * consumer: a tool that requires TypeScript as a module and calls
 * ts.resolveModuleName itself. Matching only a request of `typescript` (or one
 * ending in a `typescript` path segment) is also what keeps this preload inert
 * rather than harmful inside a tsserver process: tsserver reaches its own
 * bundle as `./typescript.js`.
 *
 * Design constraints:
 *   - Zero npm dependencies (Node.js builtins only).
 *   - Never runs Bazel: nothing here needs a `bazel` on PATH or a turn at the
 *     server lock.
 *   - Must not crash before refresh_tsconfig has ever run.
 *   - Worker thread must not block the main thread.
 */

'use strict';

const Module = require('module');
const path = require('path');

const { createResolutionSource, findWorkspaceRoot } = require('./tsserver-hook-resolver.js');

const source = createResolutionSource({
  workspaceRoot: findWorkspaceRoot(process.cwd()),
  workerPath: path.join(__dirname, 'tsserver-hook-worker.js'),
});

// ── Monkey-patch TypeScript ───────────────────────────────────────────────────
// TypeScript is loaded AFTER this --require script runs, so we intercept
// Module._load and patch the module as soon as it is first required.
//
// TypeScript >=5.0 ships its exports as non-configurable, getter-only
// properties, so direct assignment throws a TypeError and a Proxy is the only
// reliable approach.

let tsPatched = false;

const originalLoad = Module._load;

Module._load = function bazelHookLoad(request, parent, isMain) {
  const result = originalLoad.apply(this, arguments);

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
    const hit = source.resolve(moduleName);
    if (hit) return hit;

    return originalResolve.call(
      ts,
      moduleName,
      containingFile,
      compilerOptions,
      host,
      cache,
      redirectedReference,
      resolutionMode
    );
  }

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

  // Replace the module in Node's require cache so a later require('typescript')
  // gets the proxy too.
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
