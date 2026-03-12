/**
 * Pure arithmetic helpers.
 *
 * All functions carry explicit return types so that oxc-bazel can emit .d.ts
 * files without needing cross-file type inference (isolated declarations).
 */

export function add(a: number, b: number): number {
  return a + b;
}

export function multiply(a: number, b: number): number {
  return a * b;
}

export function subtract(a: number, b: number): number {
  return a - b;
}

export function divide(a: number, b: number): number {
  if (b === 0) {
    throw new RangeError("Division by zero");
  }
  return a / b;
}
