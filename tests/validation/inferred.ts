/**
 * Exports with no explicit type annotations — the shape that isolated
 * declarations rejects.
 *
 * Under declarations = "tsgo" the compiler has the full program and infers
 * these precisely. Under declarations = "oxc" the build must FAIL: oxc can
 * only see syntax, and emitting `{}` / `unknown` here would silently strip
 * the types and break consumers at their own boundaries.
 */

export const PATTERNS = {
  preview: /^preview--(.+)$/,
  project: /^project--(.+)$/,
};

export const UUID = /^[0-9a-f]{8}$/i;

export function classify(input: string) {
  if (PATTERNS.preview.test(input)) return "preview";
  if (PATTERNS.project.test(input)) return "project";
  return "other";
}
