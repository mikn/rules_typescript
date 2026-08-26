/**
 * AST helpers shared by isolated-declarations ESLint rules.
 *
 * These utilities work with @typescript-eslint/utils types so that rules can
 * inspect TypeScript-specific AST nodes without taking a direct dependency on
 * the full TypeScript compiler API.
 */

import type { TSESTree } from '@typescript-eslint/utils';

/**
 * Returns true when a function node already has an explicit return type
 * annotation (`: ReturnType` on the signature).
 */
export function hasReturnTypeAnnotation(
  node:
    | TSESTree.FunctionDeclaration
    | TSESTree.FunctionExpression
    | TSESTree.TSEmptyBodyFunctionExpression
    | TSESTree.ArrowFunctionExpression,
): boolean {
  return node.returnType != null;
}

/**
 * Returns true when a variable declarator carries an explicit type annotation
 * on the binding itself (`const x: T = ...`), regardless of its initialiser.
 */
export function hasBindingTypeAnnotation(
  declarator: TSESTree.VariableDeclarator,
): boolean {
  return declarator.id.typeAnnotation != null;
}

/**
 * Returns true when a variable declarator already has an explicit type
 * annotation (`: SomeType` on the binding) or when its initialiser is a
 * function expression / arrow function that has a return-type annotation.
 */
export function hasTypeAnnotation(
  declarator: TSESTree.VariableDeclarator,
): boolean {
  if (hasBindingTypeAnnotation(declarator)) {
    return true;
  }

  const init = declarator.init;
  if (init == null) {
    return false;
  }
  if (
    init.type === 'ArrowFunctionExpression' ||
    init.type === 'FunctionExpression'
  ) {
    return init.returnType != null;
  }

  return false;
}
