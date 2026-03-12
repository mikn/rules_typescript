/**
 * AST helpers shared by isolated-declarations ESLint rules.
 *
 * These utilities work with @typescript-eslint/utils types so that rules can
 * inspect TypeScript-specific AST nodes without taking a direct dependency on
 * the full TypeScript compiler API.
 */

import type { TSESTree } from '@typescript-eslint/utils';

// ---------------------------------------------------------------------------
// Return-type annotation helpers
// ---------------------------------------------------------------------------

/**
 * Returns true when a function node already has an explicit return type
 * annotation (`: ReturnType` on the signature).
 *
 * Handles:
 *   - `function foo(): string { ... }`
 *   - `const foo = (): string => ...`
 *   - `const foo = function(): string { ... }`
 *   - Overload signatures
 *   - Generic functions (`<T>(): T`)
 */
export function hasReturnTypeAnnotation(
  node:
    | TSESTree.FunctionDeclaration
    | TSESTree.FunctionExpression
    | TSESTree.ArrowFunctionExpression,
): boolean {
  return node.returnType != null;
}

/**
 * Returns true when a variable declarator already has an explicit type
 * annotation (`: SomeType` on the binding) or when its initialiser is a
 * function expression / arrow function that has a return-type annotation.
 *
 * Handles:
 *   - `const x: string = "hello"`
 *   - `const fn: () => string = () => "hello"`
 *   - `const fn = (): string => "hello"`  (return type on the arrow)
 */
export function hasTypeAnnotation(
  declarator: TSESTree.VariableDeclarator,
): boolean {
  // Explicit type annotation on the binding: `const x: T = ...`
  if (declarator.id.typeAnnotation != null) {
    return true;
  }

  // Return-type annotation on the initialiser when it is a function.
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

// ---------------------------------------------------------------------------
// Node classification helpers
// ---------------------------------------------------------------------------

/**
 * Returns true when the given export declaration's exported value is a
 * function (declaration or expression / arrow) that lacks an explicit
 * return-type annotation.
 *
 * Returns false for non-function exports — those are handled separately by
 * `hasTypeAnnotation`.
 */
export function isFunctionWithMissingReturnType(
  declaration: TSESTree.Node | null | undefined,
): boolean {
  if (declaration == null) {
    return false;
  }

  if (declaration.type === 'FunctionDeclaration') {
    return !hasReturnTypeAnnotation(declaration);
  }

  if (declaration.type === 'VariableDeclaration') {
    for (const declarator of declaration.declarations) {
      const init = declarator.init;
      if (
        init != null &&
        (init.type === 'ArrowFunctionExpression' ||
          init.type === 'FunctionExpression') &&
        !hasReturnTypeAnnotation(init)
      ) {
        return true;
      }
    }
  }

  return false;
}

/**
 * Returns true when the given export declaration's exported value is a
 * variable (not a function) that lacks an explicit type annotation.
 *
 * This covers patterns like:
 *   - `export const schema = z.object({...})`  — no inferred type visible
 *     to isolated declarations emit
 *   - `export const MY_MAP = new Map<string, number>()`  — type omitted
 */
export function isVariableWithMissingTypeAnnotation(
  declaration: TSESTree.Node | null | undefined,
): boolean {
  if (declaration == null || declaration.type !== 'VariableDeclaration') {
    return false;
  }

  for (const declarator of declaration.declarations) {
    const init = declarator.init;
    // Skip function initialisers — they are handled by isFunctionWithMissingReturnType.
    if (
      init != null &&
      (init.type === 'ArrowFunctionExpression' ||
        init.type === 'FunctionExpression')
    ) {
      continue;
    }
    if (!hasTypeAnnotation(declarator)) {
      return true;
    }
  }

  return false;
}

// ---------------------------------------------------------------------------
// Overload helpers
// ---------------------------------------------------------------------------

/**
 * Returns true when the given FunctionDeclaration is an overload signature
 * (it has no body).  Overload signatures are used to declare multiple call
 * signatures; the implementation signature that follows them must have an
 * explicit return type even when overloads are present.
 */
export function isOverloadSignature(
  node: TSESTree.FunctionDeclaration,
): boolean {
  return node.body == null;
}

// ---------------------------------------------------------------------------
// Class member helpers
// ---------------------------------------------------------------------------

/**
 * Returns true when the given method definition (in a class) is an overload
 * (no body).
 */
export function isMethodOverload(
  node: TSESTree.MethodDefinition,
): boolean {
  const fn = node.value;
  return fn.body == null;
}
