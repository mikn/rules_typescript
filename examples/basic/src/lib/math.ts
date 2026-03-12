/**
 * Pure arithmetic helpers.
 *
 * All functions carry explicit return types to satisfy isolatedDeclarations —
 * this allows oxc-bazel to emit .d.ts files without cross-file type inference.
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
