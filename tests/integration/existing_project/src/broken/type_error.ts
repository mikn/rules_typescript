/**
 * A real type error: the annotation says string, the expression is a number.
 *
 * Under declarations = "tsgo" the .d.ts are real build outputs, so this fails
 * a plain `bazel build` — no --output_groups=+_validation needed, and no way
 * for a broken target to hand a stale declaration to a consumer.
 */

export function addNumbers(a: number, b: number): string {
  return a + b;
}
