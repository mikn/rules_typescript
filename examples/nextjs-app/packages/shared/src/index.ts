/**
 * Shared library for the Next.js example.
 *
 * This package is compiled by ts_compile (fast, incremental, .d.ts boundary).
 * Source files are also staged into the Next.js build via staging_srcs so that
 * the Next.js app can import them via relative paths at build time.
 */

/**
 * Returns a greeting message.
 */
export function greet(name: string): string {
  return `Hello, ${name}! Built with rules_typescript.`;
}

/**
 * Formats a number as a currency string.
 */
export function formatCurrency(amount: number, currency = "USD"): string {
  return new Intl.NumberFormat("en-US", {
    style: "currency",
    currency,
  }).format(amount);
}
