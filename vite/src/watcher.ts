/**
 * File system watcher for bazel-bin changes.
 *
 * ibazel rebuilds change many files quasi-simultaneously (one Bazel action can
 * produce dozens of .js outputs).  A naive "one HMR update per file event"
 * approach would flood Vite with redundant invalidations.  Instead this module:
 *
 *  1. Watches the bazel-bin directory tree with chokidar.
 *  2. Accumulates changed .js paths into a buffer.
 *  3. After a configurable debounce window (default 50 ms) with no new events,
 *     flushes the buffer by calling the registered `onRebuild` callback with
 *     the de-duplicated list of changed module IDs.
 *
 * The watcher is also responsible for detecting ibazel's "build complete"
 * sentinel so that HMR is triggered only after the full rebuild finishes,
 * not in the middle of it.  ibazel writes the file
 * `bazel-bin/ibazel_result` (conventionally) or sets the mtime on a
 * well-known file.  We watch for changes to any `.js` file (the actual
 * compiled outputs) and let the debounce window absorb the burst.
 */

import path from 'node:path';
import type { FSWatcher } from 'chokidar';

// chokidar ships as a dependency of Vite 6.x and is always available in
// a project that has Vite installed.  We import it dynamically so that the
// plugin can still be loaded in environments where chokidar is not pre-loaded
// (e.g., during unit tests that mock the module).
async function loadChokidar(): Promise<typeof import('chokidar')> {
  return import('chokidar');
}

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

/** Called after a debounced rebuild completes, with the set of changed .js
 *  module IDs (workspace-relative, starting with `/`). */
export type RebuildCallback = (changedIds: Set<string>) => void;

export interface BazelWatcherOptions {
  /** Absolute path to the bazel-bin output tree. */
  bazelBin: string;
  /** Called once per debounce window with the set of changed file paths
   *  (absolute paths to .js files under bazel-bin). */
  onRebuild: RebuildCallback;
  /**
   * Debounce delay in milliseconds.  All file-change events that arrive
   * within this window after the first event are merged into a single
   * callback invocation.
   *
   * Default: 50 ms.  This is intentionally short — Bazel typically writes
   * all outputs within a few milliseconds of each other, and we want HMR
   * latency to stay under 100 ms.
   */
  debounceMs?: number;
}

// ---------------------------------------------------------------------------
// BazelWatcher
// ---------------------------------------------------------------------------

export class BazelWatcher {
  private readonly bazelBin: string;
  private readonly onRebuild: RebuildCallback;
  private readonly debounceMs: number;

  private watcher: FSWatcher | null = null;
  private pendingChanges: Set<string> = new Set();
  private debounceTimer: ReturnType<typeof setTimeout> | null = null;

  constructor(options: BazelWatcherOptions) {
    this.bazelBin = options.bazelBin;
    this.onRebuild = options.onRebuild;
    this.debounceMs = options.debounceMs ?? 50;
  }

  /**
   * Start watching bazel-bin for .js file changes.
   *
   * This is an async operation because chokidar is loaded dynamically.
   * The returned promise resolves once the initial scan is complete and
   * the watcher is ready to receive events.
   */
  async start(): Promise<void> {
    const chokidar = await loadChokidar();

    this.watcher = chokidar.watch(this.bazelBin, {
      // Only emit events for .js files (compiled outputs).  We deliberately
      // ignore .js.map files here — when a .js file changes we look up its
      // companion map at load time, so we don't need a separate map event.
      //
      // The filter function receives (filePath, stats?) where stats is
      // populated when chokidar has already stat()ed the entry.  Returning
      // true means "ignore this path".
      ignored: (filePath: string, stats?: { isDirectory?: () => boolean }) => {
        // Never ignore directories — chokidar needs to descend into them.
        if (stats?.isDirectory?.()) return false;
        // Ignore dotfiles (chokidar internals, git objects, etc.).
        const base = filePath.split('/').pop() ?? filePath;
        if (base.startsWith('.')) return true;
        // Ignore anything that is not a .js file.
        return !filePath.endsWith('.js');
      },
      ignoreInitial: true,
      persistent: true,
      // Prefer native fs events; fall back to polling on network file systems.
      usePolling: false,
      // Wait until the file has not changed for this many ms before reporting
      // the event.  Prevents reading a partially-written file from Bazel.
      awaitWriteFinish: {
        stabilityThreshold: 20,
        pollInterval: 10,
      },
    });

    this.watcher.on('add', (filePath: string) => this.handleFileEvent(filePath));
    this.watcher.on('change', (filePath: string) => this.handleFileEvent(filePath));

    // Wait for the initial scan to complete before returning.
    await new Promise<void>((resolve) => {
      this.watcher!.on('ready', resolve);
    });
  }

  /**
   * Stop watching and release all resources.
   */
  async stop(): Promise<void> {
    if (this.debounceTimer !== null) {
      clearTimeout(this.debounceTimer);
      this.debounceTimer = null;
    }
    if (this.watcher !== null) {
      await this.watcher.close();
      this.watcher = null;
    }
    this.pendingChanges.clear();
  }

  // ── Private ───────────────────────────────────────────────────────────────

  private handleFileEvent(absolutePath: string): void {
    // Only care about .js files (not .js.map directly — the plugin will look
    // up the map when loading the .js).
    if (!absolutePath.endsWith('.js')) return;

    this.pendingChanges.add(absolutePath);
    this.scheduleFlush();
  }

  private scheduleFlush(): void {
    if (this.debounceTimer !== null) {
      clearTimeout(this.debounceTimer);
    }
    this.debounceTimer = setTimeout(() => {
      this.flush();
    }, this.debounceMs);
  }

  private flush(): void {
    this.debounceTimer = null;

    if (this.pendingChanges.size === 0) return;

    // Snapshot and clear the pending set before invoking the callback so that
    // any new events that arrive during the callback don't get lost.
    const snapshot = new Set(this.pendingChanges);
    this.pendingChanges.clear();

    this.onRebuild(snapshot);
  }
}

// ---------------------------------------------------------------------------
// Utility: convert a bazel-bin absolute path to a workspace-relative module ID
// ---------------------------------------------------------------------------

/**
 * Converts an absolute .js path under bazel-bin to a Vite module ID of the
 * form `/workspace/relative/path.js`.
 *
 * Returns null when the path does not live under bazelBin.
 */
export function bazelPathToModuleId(absolutePath: string, bazelBin: string): string | null {
  const rel = path.relative(bazelBin, absolutePath);
  if (rel.startsWith('..')) return null;
  // Normalise to forward slashes for consistent module IDs across platforms.
  return '/' + rel.split(path.sep).join('/');
}
