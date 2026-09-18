// vite-plugin-bazel's public API: `bazelPlugin(options)` in a vite.config,
// the options BazelPluginOptions documents; docs/guides/dev-server.md.

export { bazelPlugin } from './plugin.js';
export type { BazelPluginOptions } from './plugin.js';

// Re-export lower-level utilities for consumers who need them directly
// (e.g. custom integration scripts).
export { BazelResolver } from './resolver.js';
export type { ResolverOptions, ResolvedFile, Resolution, ResolverMode } from './resolver.js';

export { BazelWatcher, ConfigWatcher, bazelPathToModuleId, digestOf } from './watcher.js';
export type {
  BazelWatcherOptions,
  ConfigInput,
  ConfigWatcherOptions,
  RebuildCallback,
  StaleCallback,
  WatchSource,
} from './watcher.js';
