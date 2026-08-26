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
 * Or pick up a bundled config:
 *
 *   export default [isolatedDeclarations.configs.recommended];
 */

import { requireExplicitTypes } from './rules/require-explicit-types.js';

const plugin = {
  meta: {
    name: '@rules_typescript/eslint-plugin-isolated-declarations',
    version: '0.1.0',
  },

  rules: {
    'require-explicit-types': requireExplicitTypes,
  },

  configs: {} as Record<string, unknown>,
};

// Built after the plugin object exists, so that the self-reference is safe.
plugin.configs['recommended'] = {
  name: 'isolated-declarations/recommended',
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
export type { RuleOptions } from './rules/require-explicit-types.js';
