/**
 * Debounced watchers over what Bazel writes: the .js files under bazel-bin, and
 * the inputs that decide whether the running Vite is still the right Vite.
 *
 * One ibazel rebuild rewrites dozens of outputs, so events are coalesced into a
 * single `onRebuild` call per quiet window instead of one HMR update per file.
 */

import crypto from 'node:crypto';
import fs from 'node:fs';
import path from 'node:path';

/**
 * The part of Vite's `server.watcher` this module uses. Vite runs that watcher
 * already and hands it to every plugin; chokidar itself is bundled inside
 * Vite's dist and is not a package a sibling module can import.
 */
export interface WatchSource {
  add(paths: string): unknown;
  on(event: 'add' | 'change', listener: (filePath: string) => void): unknown;
  off?(event: 'add' | 'change', listener: (filePath: string) => void): unknown;
  unwatch?(paths: string): unknown;
}

/** Called after a debounce window with the absolute paths of changed .js files. */
export type RebuildCallback = (changedPaths: Set<string>) => void;

export interface BazelWatcherOptions {
  /** Absolute path to the bazel-bin output tree. */
  bazelBin: string;
  onRebuild: RebuildCallback;
  /**
   * Vite's `server.watcher`. Without one, the watcher falls back to
   * `node:fs.watch`, whose recursive mode needs Node 20+ on Linux.
   */
  source?: WatchSource;
  /**
   * Quiet period before a batch is flushed, in milliseconds (default 50).
   * Bazel writes all outputs of a rebuild within a few milliseconds of each
   * other, and HMR latency should stay under 100 ms.
   */
  debounceMs?: number;
}

export class BazelWatcher {
  private readonly bazelBin: string;
  private readonly onRebuild: RebuildCallback;
  private readonly source: WatchSource | null;
  private readonly debounceMs: number;

  private readonly onFileEvent = (filePath: string): void => {
    this.handleFileEvent(filePath);
  };

  private fsWatcher: fs.FSWatcher | null = null;
  private attached: WatchSource | null = null;
  private pendingChanges: Set<string> = new Set();
  private debounceTimer: ReturnType<typeof setTimeout> | null = null;

  constructor(options: BazelWatcherOptions) {
    this.bazelBin = options.bazelBin;
    this.onRebuild = options.onRebuild;
    this.source = options.source ?? null;
    this.debounceMs = options.debounceMs ?? 50;
  }

  /**
   * Starts watching bazel-bin for .js changes.
   *
   * Rejects with an actionable message when no watch is possible, so the caller
   * can warn or fail instead of losing HMR silently.
   */
  async start(): Promise<void> {
    if (!fs.existsSync(this.bazelBin)) {
      throw new Error(
        `bazel-bin does not exist at ${this.bazelBin}. Build the target once ` +
          '(bazel build //your:target) before starting the dev server, or set the ' +
          'bazelBin option to the real output tree.',
      );
    }

    if (this.source !== null) {
      this.source.add(this.bazelBin);
      this.source.on('add', this.onFileEvent);
      this.source.on('change', this.onFileEvent);
      this.attached = this.source;
      return;
    }

    try {
      this.fsWatcher = fs.watch(
        this.bazelBin,
        { recursive: true, persistent: true },
        (_event, filename) => {
          if (filename === null) return;
          this.onFileEvent(path.resolve(this.bazelBin, filename.toString()));
        },
      );
    } catch (err) {
      const detail = err instanceof Error ? err.message : String(err);
      throw new Error(
        `cannot watch ${this.bazelBin} with node:fs.watch (${detail}). Pass Vite's ` +
          'server.watcher as the `source` option — recursive fs.watch needs Node 20+ on Linux.',
      );
    }
  }

  /** Detaches every listener. An injected source is never closed — it is Vite's. */
  async stop(): Promise<void> {
    if (this.debounceTimer !== null) {
      clearTimeout(this.debounceTimer);
      this.debounceTimer = null;
    }
    if (this.attached !== null) {
      this.attached.off?.('add', this.onFileEvent);
      this.attached.off?.('change', this.onFileEvent);
      this.attached.unwatch?.(this.bazelBin);
      this.attached = null;
    }
    if (this.fsWatcher !== null) {
      this.fsWatcher.close();
      this.fsWatcher = null;
    }
    this.pendingChanges.clear();
  }

  private handleFileEvent(absolutePath: string): void {
    if (!absolutePath.endsWith('.js')) return;
    if (bazelPathToModuleId(absolutePath, this.bazelBin) === null) return;

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

    // Snapshot before the callback so events arriving during it are not lost.
    const snapshot = new Set(this.pendingChanges);
    this.pendingChanges.clear();

    this.onRebuild(snapshot);
  }
}

/** One watched input and the digest it had when Vite last read it. */
export interface ConfigInput {
  /** Human-readable name used in the restart message. */
  label: string;
  /** Absolute path whose fingerprint is watched. */
  path: string;
  /**
   * `content` hashes the bytes. `identity` resolves the symlink instead, for
   * inputs too large to hash whose Bazel path already encodes their version.
   */
  digest: 'content' | 'identity';
  /**
   * `restart` is fixable in-process: Vite re-reads it. `manual` is not — a
   * different Node binary needs a new `bazel run`.
   */
  remedy: 'restart' | 'manual';
}

/** Named inputs that changed, in the order they were declared. */
export type StaleCallback = (changed: ConfigInput[]) => void;

export interface ConfigWatcherOptions {
  inputs: ConfigInput[];
  onStale: StaleCallback;
  /** Vite's `server.watcher`; without it nothing is watched. */
  source?: WatchSource;
  /** Quiet period before the fingerprint is recomputed (default 50 ms). */
  debounceMs?: number;
}

/**
 * Watches the inputs that generated the running Vite config.
 *
 * Vite restarts itself when its own config file changes, but it has no concept
 * of the thing that GENERATES that config — natively nothing does. Under Bazel
 * something does: `ts_dev_server` regenerates the config from BUILD deps,
 * module_name aliases, the entry point and the npm tree. A rebuild that changes
 * any of those means the running server is configured for a graph that no
 * longer exists; a rebuild that only rewrites `ts_codegen` output means it is
 * still correct and HMR handles it.
 *
 * Content digests, not timestamps, decide which of the two happened: Bazel
 * rewrites outputs on every action, so an mtime says nothing.
 */
export class ConfigWatcher {
  private readonly inputs: ConfigInput[];
  private readonly onStale: StaleCallback;
  private readonly source: WatchSource | null;
  private readonly debounceMs: number;

  private digests: Map<string, string> = new Map();
  private debounceTimer: ReturnType<typeof setTimeout> | null = null;
  private attached: WatchSource | null = null;

  private readonly onFileEvent = (): void => {
    this.scheduleCheck();
  };

  constructor(options: ConfigWatcherOptions) {
    this.inputs = options.inputs;
    this.onStale = options.onStale;
    this.source = options.source ?? null;
    this.debounceMs = options.debounceMs ?? 50;
  }

  /** The digest of every input as of now, keyed by path. */
  snapshot(): Map<string, string> {
    const digests = new Map<string, string>();
    for (const input of this.inputs) {
      digests.set(input.path, digestOf(input.path, input.digest));
    }
    return digests;
  }

  async start(): Promise<void> {
    this.digests = this.snapshot();
    if (this.source === null || this.inputs.length === 0) return;
    for (const input of this.inputs) {
      this.source.add(input.path);
    }
    this.source.on('add', this.onFileEvent);
    this.source.on('change', this.onFileEvent);
    this.attached = this.source;
  }

  async stop(): Promise<void> {
    if (this.debounceTimer !== null) {
      clearTimeout(this.debounceTimer);
      this.debounceTimer = null;
    }
    if (this.attached !== null) {
      this.attached.off?.('add', this.onFileEvent);
      this.attached.off?.('change', this.onFileEvent);
      for (const input of this.inputs) {
        this.attached.unwatch?.(input.path);
      }
      this.attached = null;
    }
  }

  /**
   * Recomputes every digest and reports the inputs that moved. Exposed so the
   * decision can be tested, and driven directly on a watcher event.
   */
  check(): ConfigInput[] {
    const next = this.snapshot();
    const changed = this.inputs.filter(
      (input) => next.get(input.path) !== this.digests.get(input.path),
    );
    this.digests = next;
    return changed;
  }

  private scheduleCheck(): void {
    if (this.debounceTimer !== null) clearTimeout(this.debounceTimer);
    this.debounceTimer = setTimeout(() => {
      this.debounceTimer = null;
      const changed = this.check();
      if (changed.length > 0) this.onStale(changed);
    }, this.debounceMs);
  }
}

/** Fingerprint of one input, or a sentinel when it cannot be read. */
export function digestOf(filePath: string, kind: 'content' | 'identity' = 'content'): string {
  try {
    if (kind === 'identity') return fs.realpathSync(filePath);
    return crypto.createHash('sha256').update(fs.readFileSync(filePath)).digest('hex');
  } catch {
    return 'absent';
  }
}

/**
 * Converts an absolute .js path under bazel-bin to a Vite module ID of the form
 * `/workspace/relative/path.js`, or null when it is not under bazelBin.
 */
export function bazelPathToModuleId(absolutePath: string, bazelBin: string): string | null {
  const rel = path.relative(bazelBin, absolutePath);
  if (rel === '' || rel.startsWith('..') || path.isAbsolute(rel)) return null;
  return '/' + rel.split(path.sep).join('/');
}
