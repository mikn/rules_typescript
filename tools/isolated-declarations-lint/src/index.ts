/**
 * eslint-plugin-isolated-declarations
 *
 * ESLint plugin that enforces explicit type annotations on all exported
 * symbols. This is the prerequisite for TypeScript's `isolatedDeclarations`
 * compiler option, which enables per-file `.d.ts` emit without a full
 * type-checking pass and is required by the `rules_typescript` Bazel ruleset.
 *
 * Usage in an ESLint 9.x flat config (eslint.config.js / eslint.config.ts):
 *
 * ```ts
 * import isolatedDeclarations from "eslint-plugin-isolated-declarations";
 *
 * export default [
 *   {
 *     plugins: {
 *       "isolated-declarations": isolatedDeclarations,
 *     },
 *     rules: {
 *       "isolated-declarations/require-explicit-types": "error",
 *     },
 *   },
 * ];
 * ```
 *
 * Or use the bundled recommended config:
 *
 * ```ts
 * import isolatedDeclarations from "eslint-plugin-isolated-declarations";
 *
 * export default [
 *   isolatedDeclarations.configs.recommended,
 * ];
 * ```
 */

import type { Linter } from "eslint";
import { requireExplicitTypes } from "./rules/require-explicit-types.js";

// ---------------------------------------------------------------------------
// Rule registry
// ---------------------------------------------------------------------------

const rules = {
  "require-explicit-types": requireExplicitTypes,
} as const;

// ---------------------------------------------------------------------------
// Configs
// ---------------------------------------------------------------------------

/** Recommended flat config. Enables all rules at the `error` severity. */
const recommended: Linter.Config = {
  name: "isolated-declarations/recommended",
  plugins: {
    // The plugin is referenced by its own name inside a flat config object.
    // Consumers that import the plugin manually choose their own namespace;
    // this config object uses "isolated-declarations" as the canonical name.
    "isolated-declarations": { rules } as unknown as Linter.Plugin,
  },
  rules: {
    "isolated-declarations/require-explicit-types": "error",
  },
};

/**
 * Strict variant: enables all rules at `error` with maximum coverage
 * (`ignoreDefaultExports: false`).
 */
const strict: Linter.Config = {
  name: "isolated-declarations/strict",
  plugins: {
    "isolated-declarations": { rules } as unknown as Linter.Plugin,
  },
  rules: {
    "isolated-declarations/require-explicit-types": [
      "error",
      { ignoreDefaultExports: false },
    ],
  },
};

// ---------------------------------------------------------------------------
// Plugin export
// ---------------------------------------------------------------------------

const plugin = {
  meta: {
    name: "eslint-plugin-isolated-declarations",
    version: "0.1.0",
  },
  rules,
  configs: {
    recommended,
    strict,
  },
};

export default plugin;

// Named exports for consumers that prefer destructuring.
export { rules };
export type { Linter };
