/**
 * Simple shared utility for the nextjs integration test.
 * This module is staged into the Next.js build action.
 */

export function greet(name: string): string {
  return `Hello, ${name}!`;
}
