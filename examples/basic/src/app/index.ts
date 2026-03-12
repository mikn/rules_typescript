/**
 * Application entry point.
 *
 * Imports from the lib package via its barrel index, exercising the cross-
 * package .d.ts compilation boundary.  The explicit return type on `main`
 * satisfies the isolatedDeclarations requirement.
 */

import { add, multiply } from "../lib";

export function main(): string {
  const sum: number = add(10, 32);
  const product: number = multiply(6, 7);
  return `Sum: ${sum}, Product: ${product}`;
}

// Run when executed directly
console.log(main());
