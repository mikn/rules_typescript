/**
 * helpers.ts - a utility module without an index.ts file.
 * Used to verify that Gazelle generates a ts_compile target for every
 * directory containing .ts files, not just those with index.ts.
 */
export function clamp(value: number, min: number, max: number): number {
  return Math.min(Math.max(value, min), max);
}

export function capitalize(s: string): string {
  if (s.length === 0) return s;
  return s[0].toUpperCase() + s.slice(1);
}
