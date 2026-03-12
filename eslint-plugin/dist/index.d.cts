import * as _typescript_eslint_utils_ts_eslint from '@typescript-eslint/utils/ts-eslint';
import { ESLintUtils } from '@typescript-eslint/utils';

/**
 * ESLint rule: `isolated-declarations/require-explicit-types`
 *
 * Reports exported bindings that lack explicit type annotations.
 * This is required for TypeScript's isolated declarations mode, which
 * enables per-file `.d.ts` emit without a type-inference pass — the
 * architectural keystone of fast incremental builds in rules_typescript.
 *
 * Without explicit return types (and explicit variable types for some
 * patterns), `tsc --isolatedDeclarations` (and oxc's isolated declarations
 * emit) cannot generate a correct `.d.ts` file from a single source file in
 * isolation.  Every implicit-return-type export is therefore a build-cache
 * invalidation risk: changing the return type of an internal helper can
 * silently change the `.d.ts` of the exporting module, forcing all
 * downstream packages to recompile.
 *
 * What this rule enforces
 * ──────────────────────
 * 1. Exported function declarations must have a `: ReturnType` annotation.
 * 2. Exported arrow function / function expression variables must have either:
 *      a. A `: () => ReturnType` annotation on the binding, OR
 *      b. A `: ReturnType` annotation on the function expression itself.
 * 3. Exported non-function variable declarations must have an explicit `: Type`
 *    annotation on the binding.
 * 4. Export-default expressions (values, not function declarations) are
 *    flagged when they have no type context.
 *
 * Edge cases handled
 * ──────────────────
 * - Overload signatures: the implementation signature must still have a
 *   return type even though the overload signatures are flagged separately.
 * - Generics: `<T>(x: T): T => x` is fine because the return type is present;
 *   `<T>(x: T) => x` is flagged.
 * - Conditional types in return position: `(): A extends B ? C : D` is fine.
 * - `export * from ...` and `export { x } from ...` re-exports are NOT
 *   flagged because they don't declare new bindings in this module.
 * - `export type` declarations are never flagged (type-only exports).
 * - `declare` ambient declarations are never flagged (they are declaration
 *   files already).
 */

type MessageId = 'missingFunctionReturnType' | 'missingVariableType' | 'missingDefaultExportType';
declare const requireExplicitTypes: ESLintUtils.RuleModule<MessageId, [], unknown, ESLintUtils.RuleListener> & {
    name: string;
};

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
declare const plugin: {
    meta: {
        name: string;
        version: string;
    };
    rules: {
        'require-explicit-types': _typescript_eslint_utils_ts_eslint.RuleModule<"missingFunctionReturnType" | "missingVariableType" | "missingDefaultExportType", [], unknown, _typescript_eslint_utils_ts_eslint.RuleListener> & {
            name: string;
        };
    };
    /**
     * Recommended configuration for ESLint flat config (ESLint 9+).
     *
     * Enables all rules at "error" severity.  This is intentionally strict:
     * isolated declarations is all-or-nothing per package.  Use the gradual
     * rollout approach (see README) to adopt incrementally.
     */
    configs: Record<string, unknown>;
};

// @ts-ignore
export = plugin;
export { plugin, requireExplicitTypes };
