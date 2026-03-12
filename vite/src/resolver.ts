/**
 * Path resolution utilities for vite-plugin-bazel.
 *
 * Responsibilities:
 *  - Map workspace-relative .ts source paths to their pre-compiled .js
 *    counterparts under bazel-bin/.
 *  - Strip the workspace/target path prefix so that a source file like
 *    `src/app/page.tsx` maps to `<bazelBin>/src/app/page.js`.
 *  - Locate companion .js.map source-map files for any .js file.
 *  - Detect whether an import ID looks like a local source file (as opposed to
 *    a bare npm specifier) so the plugin knows when to intercept resolution.
 */

import fs from 'node:fs';
import path from 'node:path';

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

export interface ResolverOptions {
  /** Absolute path to the workspace root (Vite's `root`). */
  workspaceRoot: string;
  /** Absolute path to the bazel-bin output tree. */
  bazelBin: string;
  /** Optional Bazel workspace name (currently unused but reserved for
   *  runfiles-style path construction in a future iteration). */
  workspace?: string;
}

export interface ResolvedFile {
  /** Absolute path to the .js file under bazel-bin. */
  jsPath: string;
  /** Absolute path to the .js.map file, or null if it does not exist. */
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

// ---------------------------------------------------------------------------
// Resolver class
// ---------------------------------------------------------------------------

export class BazelResolver {
  readonly workspaceRoot: string;
  readonly bazelBin: string;
  readonly workspace: string | undefined;

  constructor(options: ResolverOptions) {
    this.workspaceRoot = options.workspaceRoot;
    this.bazelBin = options.bazelBin;
    this.workspace = options.workspace;
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
   * Handles three cases:
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
  resolveId(id: string, importer?: string): ResolvedFile | null {
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
