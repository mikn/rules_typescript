/**
 * vite-plugin-bazel — public API barrel.
 *
 * Usage in vite.config.ts:
 *
 *   import { bazelPlugin } from 'vite-plugin-bazel';
 *
 *   export default defineConfig({
 *     plugins: [
 *       bazelPlugin({
 *         // Path to bazel-bin, relative to the project root.
 *         // Default: "bazel-bin"
 *         bazelBin: 'bazel-bin',
 *
 *         // Bazel target label for the dev server, used to locate the
 *         // generated node_modules tree.
 *         target: '//app:dev',
 *
 *         // Override the generated node_modules path explicitly.
 *         // nodeModules: 'bazel-bin/app/dev_node_modules',
 *
 *         // Bazel workspace name (from MODULE.bazel).
 *         // workspace: 'my_workspace',
 *
 *         // Debounce window for ibazel HMR batching (ms). Default: 50.
 *         // hmrDebounceMs: 50,
 *       }),
 *     ],
 *   });
 */

export { bazelPlugin } from './plugin.js';
export type { BazelPluginOptions } from './plugin.js';

// Re-export lower-level utilities for consumers who need them directly
// (e.g. custom integration scripts).
export { BazelResolver } from './resolver.js';
export type { ResolverOptions, ResolvedFile } from './resolver.js';

export { BazelWatcher, bazelPathToModuleId } from './watcher.js';
export type { BazelWatcherOptions, RebuildCallback } from './watcher.js';
