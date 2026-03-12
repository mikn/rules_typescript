/**
 * Application entry point.
 *
 * Imports from the lib package via its barrel index, exercising the cross-
 * package .d.ts compilation boundary.  The explicit return type on `main`
 * satisfies the isolatedDeclarations requirement.
 */

import { add, multiply } from "../lib";

export function main(): string {
  const sum: number = add(1, 2);
  const product: number = multiply(3, 4);
  return `Result: ${sum} and ${product}`;
}
