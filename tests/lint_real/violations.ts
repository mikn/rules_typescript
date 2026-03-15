// violations.ts — TypeScript file with intentional lint violations.
// The no-var rule is enabled via oxlint.json; this file uses 'var' which triggers it.
// Used by the real linter test to verify violations cause lint failure.

// oxlint(no-var): Unexpected var, use let or const instead.
var x = 1;

export { x };
