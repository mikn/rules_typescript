/**
 * Path resolution for vite-plugin-bazel.
 *
 * Two modes, because dev and prod want opposite things from the same import.
 *
 * `build` — every first-party `.ts`/`.tsx` import is redirected to the
 * pre-compiled `.js` Bazel already wrote under bazel-bin. Bazel owns the
 * transform; Vite only links. This is the mode the plugin has always had, and
 * `resolveIdForBuild` below is that code unchanged.
 *
 * `serve` — checked-in first-party source is handed back to Vite to transform
 * in memory, which is what takes a Bazel analysis+action cycle out of the
 * keystroke-to-browser path. bazel-bin stays authoritative for what Vite
 * cannot produce itself: `ts_codegen` outputs (route trees, generated protos)
 * and generated assets.
 *
 * The dev decision is made per file by asking the filesystem, not by matching a
 * pattern: a module whose source is checked in is served from source, and a
 * module with no source in the workspace is by construction a build output. No
 * second list of generated paths to drift out of sync with `ts_codegen`.
 */

import fs from 'node:fs';
import path from 'node:path';

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

/** Which half of the ruleset is driving: `vite dev` or `vite build`. */
export type ResolverMode = 'serve' | 'build';

export interface ResolverOptions {
  /** Absolute path to the workspace root (Vite's `root`). */
  workspaceRoot: string;
  /** Absolute path to the bazel-bin output tree. */
  bazelBin: string;
  /** Optional Bazel workspace name (currently unused but reserved for
   *  runfiles-style path construction in a future iteration). */
  workspace?: string | undefined;
  /** Defaults to `build`: redirect everything to bazel-bin, as before. */
  mode?: ResolverMode | undefined;
}

export interface ResolvedFile {
  /** Absolute path to the .js file under bazel-bin. */
  jsPath: string;
  /** Absolute path to the .js.map file, or null if it does not exist. */
  mapPath: string | null;
}

/** Where an import specifier landed, and who is expected to transform it. */
export interface Resolution {
  /** Absolute path of the file Vite should load. */
  filePath: string;
  /**
   * True when `filePath` is a Bazel `.js` output to be served verbatim with its
   * `.js.map`; false when it is source for Vite to transform.
   */
  precompiled: boolean;
  /** Absolute `.js.map` path, when one exists next to a precompiled output. */
  mapPath: string | null;
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/**
 * Returns true when the import ID is definitely a relative path (starts with
 * `.` or `..`) rather than a bare specifier or absolute path.
 */
export function isRelativeImport(id: string): boolean {
  return id.startsWith('./') || id.startsWith('../');
}

/**
 * Returns true when the path looks like a TypeScript source file that should
 * be intercepted and redirected to its bazel-bin counterpart.
 *
 * We catch explicit .ts / .tsx extensions but explicitly exclude .d.ts files
 * (ambient declaration files that do not have a .js counterpart).
 */
export function isTsSourcePath(filePath: string): boolean {
  // Exclude .d.ts — these are declaration-only files with no .js output.
  if (filePath.endsWith('.d.ts')) return false;
  return filePath.endsWith('.ts') || filePath.endsWith('.tsx');
}

/**
 * Strips the TypeScript extension from a file path and replaces it with `.js`.
 *
 * Examples:
 *   "src/app/page.ts"   → "src/app/page.js"
 *   "src/app/page.tsx"  → "src/app/page.js"
 *   "src/app/page.d.ts" → "src/app/page.d.js"  (caller must handle .d.ts)
 */
export function tsPathToJsPath(tsPath: string): string {
  return tsPath.replace(/\.tsx?$/, '.js');
}

/**
 * The TypeScript sources a `.js` specifier can mean. TypeScript's node16 and
 * nodenext ESM output requires `./foo.js` in the source text even though the
 * file on disk is `foo.ts`, so a dev server serving source has to invert it.
 */
export function jsPathToTsCandidates(jsPath: string): string[] {
  if (!jsPath.endsWith('.js')) return [];
  const stem = jsPath.slice(0, -3);
  return [stem + '.ts', stem + '.tsx'];
}

// ---------------------------------------------------------------------------
// Resolver class
// ---------------------------------------------------------------------------

export class BazelResolver {
  readonly workspaceRoot: string;
  readonly bazelBin: string;
  readonly workspace: string | undefined;
  readonly mode: ResolverMode;

  constructor(options: ResolverOptions) {
    this.workspaceRoot = options.workspaceRoot;
    this.bazelBin = options.bazelBin;
    this.workspace = options.workspace;
    this.mode = options.mode ?? 'build';
  }

  /**
   * Given an absolute path to a TypeScript source file, returns the absolute
   * path to the corresponding pre-compiled .js file under bazel-bin, along
   * with its source-map path if present.
   *
   * Returns null when the source file does not have a known bazel-bin
   * counterpart (e.g. the file is outside the workspace root).
   */
  resolveSourceToJs(absoluteTsPath: string): ResolvedFile | null {
    // Only handle .ts/.tsx source files.
    if (!isTsSourcePath(absoluteTsPath)) return null;

    // Compute the workspace-relative path of the source file.
    const rel = path.relative(this.workspaceRoot, absoluteTsPath);

    // Bail out if the path escapes the workspace root (contains leading `..`).
    if (rel.startsWith('..')) return null;

    // Build the bazel-bin path by replacing the TypeScript extension with .js.
    const relJs = tsPathToJsPath(rel);
    const jsPath = path.join(this.bazelBin, relJs);

    return {
      jsPath,
      mapPath: this.findMapForJs(jsPath),
    };
  }

  /**
   * Given an absolute path to a .js file under bazel-bin, returns its
   * workspace-relative path (suitable for use as a Vite module ID).
   */
  jsPathToModuleId(absoluteJsPath: string): string | null {
    const rel = path.relative(this.bazelBin, absoluteJsPath);
    if (rel.startsWith('..')) return null;
    // Vite module IDs use forward slashes.
    return '/' + rel.split(path.sep).join('/');
  }

  /**
   * Given an absolute path to a .js file, returns the absolute path to the
   * companion .js.map file if it exists on disk, otherwise null.
   */
  findMapForJs(jsPath: string): string | null {
    const mapPath = jsPath + '.map';
    return fs.existsSync(mapPath) ? mapPath : null;
  }

  /**
   * Resolves a module `id` as seen by Vite's `resolveId` hook.
   *
   * Bare specifiers always return null in both modes: npm packages and
   * first-party `module_name` packages are resolved through `resolve.alias` in
   * the generated config, which is where the Bazel-computed mapping lives.
   */
  resolveId(id: string, importer?: string): Resolution | null {
    if (this.mode === 'build') {
      const built = this.resolveIdForBuild(id, importer);
      return built === null
        ? null
        : { filePath: built.jsPath, precompiled: true, mapPath: built.mapPath };
    }
    return this.resolveIdForServe(id, importer);
  }

  /**
   * Handles four cases:
   *  1. Absolute path pointing into the workspace source tree → redirect to
   *     its bazel-bin .js counterpart.
   *  2. Relative import whose importer lives in the workspace source tree →
   *     resolve relative to importer directory, then redirect.
   *  3. Relative import whose importer is already a bazel-bin .js file →
   *     resolve the import relative to both the source tree and bazel-bin,
   *     preferring the bazel-bin .js output when it exists.
   *  4. Anything else (bare specifier, non-ts absolute path, etc.) →
   *     return null to let Vite's default resolver take over.
   */
  private resolveIdForBuild(id: string, importer?: string): ResolvedFile | null {
    // ── Case 1: absolute path into source tree ────────────────────────────
    if (path.isAbsolute(id)) {
      return this.resolveSourceToJs(id);
    }

    // ── Cases 2 & 3: relative import ─────────────────────────────────────
    if (!isRelativeImport(id) || importer == null) return null;

    const importerDir = path.dirname(importer);
    const importerIsInBazelBin = importerDir.startsWith(this.bazelBin + path.sep)
      || importerDir === this.bazelBin;

    // ── Case 3: importer is a bazel-bin .js file ──────────────────────────
    // Relative .js-to-.js imports within bazel-bin are already resolved
    // correctly by Vite's default file-system resolver — we don't need to
    // intercept them.  But if the specifier has no extension (or a .ts
    // extension from source-authored code), we need to probe for the .js
    // output.
    if (importerIsInBazelBin) {
      const candidates = buildExtensionCandidates(id);
      for (const candidate of candidates) {
        const absInBazelBin = path.resolve(importerDir, candidate);
        // If the candidate path ends in .ts/.tsx, map it to its .js output.
        if (isTsSourcePath(absInBazelBin)) {
          // Derive the source-tree path from the bazel-bin path.
          const relFromBazelBin = path.relative(this.bazelBin, absInBazelBin);
          const sourceAbsolute = path.join(this.workspaceRoot, relFromBazelBin);
          const result = this.resolveSourceToJs(sourceAbsolute);
          if (result !== null && fs.existsSync(result.jsPath)) {
            return result;
          }
        } else if (absInBazelBin.endsWith('.js')) {
          // Already a .js path — only intercept if it lives under bazel-bin.
          if (absInBazelBin.startsWith(this.bazelBin + path.sep) && fs.existsSync(absInBazelBin)) {
            return { jsPath: absInBazelBin, mapPath: this.findMapForJs(absInBazelBin) };
          }
        }
      }
      return null;
    }

    // ── Case 2: importer is a source-tree .ts file ────────────────────────
    const candidates = buildExtensionCandidates(id);

    for (const candidate of candidates) {
      const absolute = path.resolve(importerDir, candidate);

      if (isTsSourcePath(absolute)) {
        const result = this.resolveSourceToJs(absolute);
        if (result !== null && fs.existsSync(result.jsPath)) {
          return result;
        }
      }
    }

    return null;
  }

  /**
   * Dev resolution. Every candidate is expressed as a workspace-source path
   * first — a bazel-bin importer's relative specifier included — so that a
   * generated module and a hand-written one importing the same file end up on
   * the same module in Vite's graph rather than two copies of it.
   */
  private resolveIdForServe(id: string, importer?: string): Resolution | null {
    if (path.isAbsolute(id)) {
      return this.classify(this.sourcePathForBinPath(id) ?? id);
    }

    if (!isRelativeImport(id) || importer == null) return null;

    const importerDir = path.dirname(importer);
    const resolveFrom = this.sourcePathForBinPath(importerDir) ?? importerDir;

    for (const candidate of buildExtensionCandidates(id)) {
      const resolution = this.classify(path.resolve(resolveFrom, candidate));
      if (resolution !== null) return resolution;
    }
    return null;
  }

  /**
   * Decides who owns a workspace-relative path in dev: Vite (source on disk),
   * bazel-bin (no source — therefore generated), or nobody (null, so Vite's
   * own resolver takes over).
   */
  private classify(sourcePath: string): Resolution | null {
    if (!this.underWorkspace(sourcePath)) return null;

    // `./foo.js` out of TypeScript source means `foo.ts` on disk.
    for (const candidate of jsPathToTsCandidates(sourcePath)) {
      if (fs.existsSync(candidate)) {
        return { filePath: candidate, precompiled: false, mapPath: null };
      }
    }

    if (fs.existsSync(sourcePath)) {
      // A checked-in asset or .d.ts is not ours: Vite serves it from source.
      if (!isTsSourcePath(sourcePath)) return null;
      return { filePath: sourcePath, precompiled: false, mapPath: null };
    }

    // No source on disk, so whatever this is, Bazel generated it.
    for (const candidate of this.generatedCandidates(sourcePath)) {
      if (!fs.existsSync(candidate)) continue;
      const precompiled = candidate.endsWith('.js');
      return {
        filePath: candidate,
        precompiled,
        mapPath: precompiled ? this.findMapForJs(candidate) : null,
      };
    }
    return null;
  }

  /**
   * bazel-bin paths a missing source path can mean: the generated source
   * itself (`ts_codegen` writes .ts, which Vite can transform) and the
   * compiled output of it.
   */
  private generatedCandidates(sourcePath: string): string[] {
    const direct = this.binPathForSourcePath(sourcePath);
    if (direct === null) return [];
    if (!isTsSourcePath(sourcePath)) return [direct];
    return [direct, tsPathToJsPath(direct)];
  }

  /** Same workspace-relative path, rooted at bazel-bin instead. */
  private binPathForSourcePath(sourcePath: string): string | null {
    const rel = path.relative(this.workspaceRoot, sourcePath);
    if (rel === '' || rel.startsWith('..') || path.isAbsolute(rel)) return null;
    return path.join(this.bazelBin, rel);
  }

  /** Inverse of binPathForSourcePath; null when the path is not under bazel-bin. */
  private sourcePathForBinPath(binPath: string): string | null {
    const rel = path.relative(this.bazelBin, binPath);
    if (rel === '' || rel.startsWith('..') || path.isAbsolute(rel)) return null;
    return path.join(this.workspaceRoot, rel);
  }

  private underWorkspace(absolute: string): boolean {
    const rel = path.relative(this.workspaceRoot, absolute);
    return rel !== '' && !rel.startsWith('..') && !path.isAbsolute(rel);
  }
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

/**
 * Given a raw import specifier, returns a list of candidate paths to probe
 * when searching for the backing source file.  The specifier may or may not
 * carry an explicit extension.
 */
function buildExtensionCandidates(specifier: string): string[] {
  const hasExplicitExtension = /\.[a-z]+$/i.test(specifier);

  if (hasExplicitExtension) {
    // Already has an extension — only try as-is.
    return [specifier];
  }

  return [
    specifier + '.ts',
    specifier + '.tsx',
    specifier + '/index.ts',
    specifier + '/index.tsx',
  ];
}
