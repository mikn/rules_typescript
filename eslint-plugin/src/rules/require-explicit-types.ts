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

import { ESLintUtils, TSESTree, AST_NODE_TYPES } from '@typescript-eslint/utils';
import {
  hasReturnTypeAnnotation,
  hasTypeAnnotation,
  isOverloadSignature,
} from '../utils.js';

// ---------------------------------------------------------------------------
// Message IDs
// ---------------------------------------------------------------------------

type MessageId =
  | 'missingFunctionReturnType'
  | 'missingVariableType'
  | 'missingDefaultExportType';

// ---------------------------------------------------------------------------
// Rule definition
// ---------------------------------------------------------------------------

const createRule = ESLintUtils.RuleCreator(
  (name) =>
    `https://github.com/mikn/rules_typescript/blob/main/eslint-plugin/docs/rules/${name}.md`,
);

export const requireExplicitTypes = createRule<[], MessageId>({
  name: 'require-explicit-types',
  meta: {
    type: 'problem',
    docs: {
      description:
        'Require explicit type annotations on exported bindings for isolated declarations compatibility',
    },
    messages: {
      missingFunctionReturnType:
        "Exported function '{{name}}' is missing an explicit return type annotation. " +
        'Add a return type (e.g. `function {{name}}(): ReturnType`) to enable isolated ' +
        'declarations emit without a type-inference pass. ' +
        'See: https://www.typescriptlang.org/tsconfig#isolatedDeclarations',

      missingVariableType:
        "Exported variable '{{name}}' is missing an explicit type annotation. " +
        'Add a type annotation (e.g. `const {{name}}: SomeType = ...`) so that the ' +
        '.d.ts can be emitted without type inference. ' +
        'See: https://www.typescriptlang.org/tsconfig#isolatedDeclarations',

      missingDefaultExportType:
        'Default export is missing an explicit type annotation. ' +
        'Wrap in a typed variable (`const value: Type = ...; export default value`) ' +
        'or add a return-type annotation to the function. ' +
        'See: https://www.typescriptlang.org/tsconfig#isolatedDeclarations',
    },
    schema: [],
  },
  defaultOptions: [],

  create(context) {
    // ── Named export declarations ────────────────────────────────────────
    function checkExportNamedDeclaration(
      node: TSESTree.ExportNamedDeclaration,
    ): void {
      const { declaration } = node;
      if (declaration == null) {
        // `export { x }` or `export { x } from "..."` — re-export, skip.
        return;
      }

      // `export type { ... }` — type-only, skip.
      if (node.exportKind === 'type') {
        return;
      }

      if (declaration.type === AST_NODE_TYPES.FunctionDeclaration) {
        // Skip overload signatures (no body) — the implementation signature
        // that follows will be checked in its own ExportNamedDeclaration.
        if (isOverloadSignature(declaration)) {
          return;
        }
        if (!hasReturnTypeAnnotation(declaration)) {
          const funcName =
            declaration.id?.name ?? '<anonymous>';
          context.report({
            node: declaration,
            messageId: 'missingFunctionReturnType',
            data: { name: funcName },
          });
        }
        return;
      }

      // TSDeclareFunction covers `export declare function foo(): void` — skip.
      if (declaration.type === AST_NODE_TYPES.TSDeclareFunction) {
        return;
      }

      if (declaration.type === AST_NODE_TYPES.VariableDeclaration) {
        // Skip `declare const` / `declare let` — ambient declarations.
        if ((declaration as TSESTree.VariableDeclaration & { declare?: boolean }).declare === true) {
          return;
        }

        for (const declarator of declaration.declarations) {
          const init = declarator.init;
          const bindingName = getBindingName(declarator.id);

          // Function expression / arrow function: check for return type on
          // the function itself OR a full type annotation on the binding.
          if (
            init != null &&
            (init.type === AST_NODE_TYPES.ArrowFunctionExpression ||
              init.type === AST_NODE_TYPES.FunctionExpression)
          ) {
            const hasBindingType = declarator.id.typeAnnotation != null;
            const hasFunctionReturnType = hasReturnTypeAnnotation(init);
            if (!hasBindingType && !hasFunctionReturnType) {
              context.report({
                node: declarator,
                messageId: 'missingFunctionReturnType',
                data: { name: bindingName },
              });
            }
            continue;
          }

          // Non-function variable: must have an explicit binding type.
          if (!hasTypeAnnotation(declarator)) {
            context.report({
              node: declarator,
              messageId: 'missingVariableType',
              data: { name: bindingName },
            });
          }
        }
        return;
      }

      // TSTypeAliasDeclaration, TSInterfaceDeclaration, TSEnumDeclaration,
      // ClassDeclaration — these are their own types and isolated declarations
      // handles them differently; don't flag.
    }

    // ── Default export declarations ──────────────────────────────────────
    function checkExportDefaultDeclaration(
      node: TSESTree.ExportDefaultDeclaration,
    ): void {
      // ExportDefaultDeclaration.exportKind is always 'value' in the current
      // @typescript-eslint AST spec.  The check below is kept as a guard for
      // future spec changes but is currently unreachable.

      const { declaration } = node;

      if (declaration.type === AST_NODE_TYPES.FunctionDeclaration) {
        // Overload signatures are not directly emitted as default exports,
        // but if the function has no body it's a bare overload — skip.
        if (isOverloadSignature(declaration)) {
          return;
        }
        if (!hasReturnTypeAnnotation(declaration)) {
          const funcName = declaration.id?.name ?? 'default';
          context.report({
            node: declaration,
            messageId: 'missingFunctionReturnType',
            data: { name: funcName },
          });
        }
        return;
      }

      if (declaration.type === AST_NODE_TYPES.ArrowFunctionExpression) {
        if (!hasReturnTypeAnnotation(declaration)) {
          context.report({
            node: declaration,
            messageId: 'missingDefaultExportType',
          });
        }
        return;
      }

      // `export default <identifier>` — the identifier was declared
      // elsewhere; we don't flag it here since the declaration site is
      // responsible for the type annotation.
      if (declaration.type === AST_NODE_TYPES.Identifier) {
        return;
      }

      // `export default <literal>` — literals are self-typed; skip.
      if (
        declaration.type === AST_NODE_TYPES.Literal ||
        declaration.type === AST_NODE_TYPES.TemplateLiteral
      ) {
        return;
      }

      // `export default class { ... }` — classes are handled by tsc.
      if (declaration.type === AST_NODE_TYPES.ClassDeclaration) {
        return;
      }

      // TypeScript-specific declaration types are self-describing.
      if (
        declaration.type === AST_NODE_TYPES.TSDeclareFunction ||
        declaration.type === AST_NODE_TYPES.TSInterfaceDeclaration ||
        declaration.type === AST_NODE_TYPES.TSTypeAliasDeclaration ||
        declaration.type === AST_NODE_TYPES.TSEnumDeclaration ||
        declaration.type === AST_NODE_TYPES.TSModuleDeclaration
      ) {
        return;
      }

      // `export default <expression>` without a type context — flag it.
      // This catches patterns like `export default { foo: 1 }` where the
      // object literal type isn't statically knowable without inference.
      context.report({
        node: declaration,
        messageId: 'missingDefaultExportType',
      });
    }

    return {
      ExportNamedDeclaration: checkExportNamedDeclaration,
      ExportDefaultDeclaration: checkExportDefaultDeclaration,
    };
  },
});

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/**
 * Extracts a human-readable name from a binding pattern for error messages.
 *
 * Returns the identifier name for simple bindings, or a placeholder for
 * destructuring patterns.
 */
function getBindingName(node: TSESTree.BindingName): string {
  if (node.type === AST_NODE_TYPES.Identifier) {
    return node.name;
  }
  // Destructuring patterns: `const { a, b } = ...` — just return a generic name.
  return '<destructured>';
}
