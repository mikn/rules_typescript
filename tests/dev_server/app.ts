/**
 * Minimal TypeScript application entry point for the dev server test.
 *
 * This file exists only to provide a ts_compile target for ts_dev_server
 * to consume.  It does not need to be a full application.
 */

export function greet(name: string): string {
  return `Hello, ${name}!`;
}

// Entry point when served directly.
const el = document.getElementById("app");
if (el) {
  el.textContent = greet("Bazel");
}
