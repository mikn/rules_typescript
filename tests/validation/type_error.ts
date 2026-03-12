/**
 * Deliberately incorrect TypeScript — used to verify that tsgo
 * type-checking FAILS on type errors.
 *
 * The return type annotation says "string" but the expression produces a
 * "number". tsgo should flag this as a type error.
 */

export function addNumbers(a: number, b: number): string {
  // Type error: number is not assignable to string
  return a + b;
}
