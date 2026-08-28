/**
 * No explicit export types. oxc cannot infer them, so building this target
 * under declarations = "oxc" must FAIL rather than widen them to `{}` and
 * `unknown` — a silent widening only surfaces later, in a consumer, against
 * the wrong file.
 */

export const PATTERNS = {
  preview: /^preview--(.+)$/,
  project: /^project--(.+)$/,
};

export function classify(input: string) {
  return PATTERNS.preview.test(input) ? "preview" : "other";
}
