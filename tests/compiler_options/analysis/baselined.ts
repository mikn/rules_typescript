// A source of its own rather than lone.ts: two ts_compile targets over one
// source declare the same lone.js, and this one is analysed in the target
// configuration where lone.ts's other non-failing target already is.
//
// Strict-clean, because the ruleset baseline reaches this target under a
// tsconfig that says nothing about strict, and free of unused locals, which
// that tsconfig does ask for.
export function widen(value: string): string {
  return value;
}
