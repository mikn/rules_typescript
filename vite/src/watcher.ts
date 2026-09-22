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
        `cannot watch ${this.bazelBin} with node:fs.watch (${detail}); ` +
          'recursive watching requires Node 20+ on Linux.',
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
  /** Quiet period before the fingerprint is recomputed (default 50 ms). */
  debounceMs?: number;
}

// Watches the inputs that generated the running Vite config, which Vite itself
// does not know of. Digests, not mtimes: each action run rewrites its outputs.
export class ConfigWatcher {
  private readonly inputs: ConfigInput[];
  private readonly onStale: StaleCallback;
  private readonly debounceMs: number;

  private digests: Map<string, string> = new Map();
  private debounceTimer: ReturnType<typeof setTimeout> | null = null;
  private watchers: fs.FSWatcher[] = [];

  private readonly onFileEvent = (): void => {
    this.scheduleCheck();
  };

  constructor(options: ConfigWatcherOptions) {
    this.inputs = options.inputs;
    this.onStale = options.onStale;
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
    const directories = new Map<string, Set<string>>();
    for (const input of this.inputs) {
      const directory = path.dirname(input.path);
      const names = directories.get(directory) ?? new Set<string>();
      names.add(path.basename(input.path));
      directories.set(directory, names);
    }
    try {
      for (const [directory, names] of directories) {
        this.watchers.push(fs.watch(directory, (_event, filename) => {
          if (filename === null || names.has(filename.toString())) this.onFileEvent();
        }));
      }
    } catch (err) {
      await this.stop();
      throw err;
    }
  }

  async stop(): Promise<void> {
    if (this.debounceTimer !== null) {
      clearTimeout(this.debounceTimer);
      this.debounceTimer = null;
    }
    for (const watcher of this.watchers) watcher.close();
    this.watchers = [];
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
