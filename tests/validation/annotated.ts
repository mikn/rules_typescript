/**
 * The same shapes as inferred.ts, but with an explicit type on every export —
 * what declarations = "oxc" requires. Kept separate from correct.ts because
 * ts_compile derives its output paths from the source file name, so two
 * targets in one package cannot compile the same source.
 */

export const PATTERNS: { preview: RegExp; project: RegExp } = {
  preview: /^preview--(.+)$/,
  project: /^project--(.+)$/,
};

export const UUID: RegExp = /^[0-9a-f]{8}$/i;

export function classify(input: string): "preview" | "project" | "other" {
  if (PATTERNS.preview.test(input)) return "preview";
  if (PATTERNS.project.test(input)) return "project";
  return "other";
}
