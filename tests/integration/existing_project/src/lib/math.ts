/**
 * Pure arithmetic helpers WITHOUT explicit return types.
 * This tests that isolated_declarations = false allows these to compile
 * even though TypeScript cannot infer the return type in isolation.
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
