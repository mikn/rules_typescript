/**
 * @rules_typescript/eslint-plugin-isolated-declarations
 *
 * ESLint plugin that enforces explicit type annotations on exported bindings,
 * enabling TypeScript's isolated declarations mode for fast per-file .d.ts
 * emit.
 *
 * Usage (ESLint flat config — ESLint 9+):
 *
 *   import isolatedDeclarations from '@rules_typescript/eslint-plugin-isolated-declarations';
 *
 *   export default [
 *     {
 *       plugins: {
 *         'isolated-declarations': isolatedDeclarations,
 *       },
 *       rules: {
 *         'isolated-declarations/require-explicit-types': 'error',
 *       },
 *     },
 *   ];
 *
 * Usage (legacy .eslintrc — ESLint 8):
 *
 *   {
 *     "plugins": ["@rules_typescript/isolated-declarations"],
 *     "rules": {
 *       "@rules_typescript/isolated-declarations/require-explicit-types": "error"
 *     }
 *   }
 */

import { requireExplicitTypes } from './rules/require-explicit-types.js';

// ---------------------------------------------------------------------------
// Plugin definition
// ---------------------------------------------------------------------------

const plugin = {
  meta: {
    name: '@rules_typescript/eslint-plugin-isolated-declarations',
    version: '0.1.0',
  },

  rules: {
    'require-explicit-types': requireExplicitTypes,
  },

  /**
   * Recommended configuration for ESLint flat config (ESLint 9+).
   *
   * Enables all rules at "error" severity.  This is intentionally strict:
   * isolated declarations is all-or-nothing per package.  Use the gradual
   * rollout approach (see README) to adopt incrementally.
   */
  configs: {} as Record<string, unknown>,
};

// Build the recommended config after the plugin object is created, so that
// self-reference is safe.
plugin.configs['recommended'] = {
  plugins: {
    'isolated-declarations': plugin,
  },
  rules: {
    'isolated-declarations/require-explicit-types': 'error',
  },
};

export default plugin;

// Named export for tooling that prefers it.
export { plugin };

// Re-export individual rules for consumers who want fine-grained control.
export { requireExplicitTypes } from './rules/require-explicit-types.js';
