/**
 * Pure arithmetic helpers WITHOUT explicit return types — the shape isolated
 * declarations rejects and that real codebases are full of.
 *
 * Under the default declarations = "tsgo" these compile unmodified AND their
 * emitted .d.ts carry the inferred `number` return types. A syntactic emitter
 * would have had to widen them.
 */

export function add(a: number, b: number) {
  return a + b;
}

export function multiply(a: number, b: number) {
  return a * b;
}

export function subtract(a: number, b: number) {
  return a - b;
}

export function divide(a: number, b: number) {
  if (b === 0) {
    throw new RangeError("Division by zero");
  }
  return a / b;
}
