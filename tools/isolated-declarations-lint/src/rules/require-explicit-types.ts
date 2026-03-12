/**
 * ESLint rule: require-explicit-types
 *
 * Reports all exported symbols that are missing explicit type annotations.
 * This is a prerequisite for TypeScript's `isolatedDeclarations` compiler
 * option, which enables per-file `.d.ts` emit without a full type-check pass.
 *
 * Provides auto-fix for types that are directly inferrable from the AST:
 *   - Literal primitives (string, number, boolean)
 *   - `null` and `undefined`
 *   - Uniform-element array literals
 *   - Arrow functions and function declarations with literal return values
 *
 * For types that require a type-checker (e.g. call expressions, complex
 * destructuring) the rule reports a clear error message pointing the developer
 * toward a manual annotation.
 *
 * Targets ESLint 9.x flat config with @typescript-eslint/utils v8.
 */

import { AST_NODE_TYPES, TSESTree } from "@typescript-eslint/utils";
import { RuleContext, RuleListener } from "@typescript-eslint/utils/ts-eslint";
import { createRule } from "../utils.js";

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

type MessageIds =
  | "missingReturnType"
  | "missingVariableType"
  | "missingParameterType"
  | "missingPropertyType"
  | "addReturnType"
  | "addVariableType"
  | "cannotInferType";

interface RuleOptions {
  /** Allow missing types on exported `default` if the expression is a class or
   *  function declaration — those are already named and thus inferrable by tsc.
   *  Defaults to `false` for maximum strictness. */
  ignoreDefaultExports?: boolean;
}

// ---------------------------------------------------------------------------
// Literal-type inference helpers
// ---------------------------------------------------------------------------

/** Returns the TypeScript type literal string for a simple expression node, or
 *  `null` when the type cannot be inferred purely from the AST. */
function inferLiteralType(node: TSESTree.Expression): string | null {
  switch (node.type) {
    case AST_NODE_TYPES.Literal: {
      const lit = node as TSESTree.Literal;
      if (typeof lit.value === "string") return "string";
      if (typeof lit.value === "number") return "number";
      if (typeof lit.value === "boolean") return "boolean";
      if (lit.value === null) return "null";
      // BigInt literal
      if ("bigint" in lit && lit.bigint !== undefined) return "bigint";
      return null;
    }

    case AST_NODE_TYPES.TemplateLiteral:
      // A template literal with no expressions is a string constant.
      // With expressions we can still say `string` because the overall
      // type is always `string`.
      return "string";

    case AST_NODE_TYPES.UnaryExpression: {
      const unary = node as TSESTree.UnaryExpression;
      if (unary.operator === "void") return "undefined";
      // `-1`, `+1` etc. are numeric
      if (
        (unary.operator === "-" || unary.operator === "+") &&
        unary.argument.type === AST_NODE_TYPES.Literal &&
        typeof (unary.argument as TSESTree.Literal).value === "number"
      ) {
        return "number";
      }
      if (
        unary.operator === "!" &&
        unary.argument.type === AST_NODE_TYPES.Literal
      ) {
        return "boolean";
      }
      return null;
    }

    case AST_NODE_TYPES.Identifier: {
      const id = node as TSESTree.Identifier;
      if (id.name === "undefined") return "undefined";
      if (id.name === "NaN" || id.name === "Infinity") return "number";
      return null;
    }

    case AST_NODE_TYPES.ArrayExpression: {
      const arr = node as TSESTree.ArrayExpression;
      if (arr.elements.length === 0) return "never[]";
      // Uniform element type inference
      const elementTypes = new Set<string>();
      for (const el of arr.elements) {
        if (el === null) {
          // Sparse array — too complex
          return null;
        }
        if (el.type === AST_NODE_TYPES.SpreadElement) {
          // Spread element — too complex
          return null;
        }
        const elType = inferLiteralType(el as TSESTree.Expression);
        if (elType === null) return null;
        elementTypes.add(elType);
      }
      if (elementTypes.size === 1) {
        const [elType] = [...elementTypes];
        return `${elType}[]`;
      }
      // Multiple primitive types → union array
      if (elementTypes.size <= 4) {
        const union = [...elementTypes].sort().join(" | ");
        return `(${union})[]`;
      }
      return null;
    }

    case AST_NODE_TYPES.ObjectExpression: {
      // We intentionally do NOT infer object literal types because the resulting
      // annotation would be structurally verbose and likely wrong (e.g. it
      // wouldn't capture optional properties or methods properly). Callers
      // should annotate object exports manually.
      return null;
    }

    default:
      return null;
  }
}

/** Infers the return type of a function by examining its body.
 *
 *  Only handles the trivial cases:
 *  - A single return statement whose expression has an inferrable literal type.
 *  - A body-less arrow function (expression body) whose expression has an
 *    inferrable literal type.
 *
 *  Returns `null` when the return type cannot be inferred purely from the AST.
 */
function inferReturnType(
  body: TSESTree.BlockStatement | TSESTree.Expression
): string | null {
  if (body.type === AST_NODE_TYPES.BlockStatement) {
    const block = body as TSESTree.BlockStatement;
    // Only handle a single-statement body with a return.
    if (block.body.length !== 1) return null;
    const stmt = block.body[0];
    if (
      stmt === undefined ||
      stmt.type !== AST_NODE_TYPES.ReturnStatement
    ) {
      return null;
    }
    const ret = stmt as TSESTree.ReturnStatement;
    if (ret.argument === null || ret.argument === undefined) return "void";
    return inferLiteralType(ret.argument);
  }

  // Expression body (arrow function)
  if (isExpression(body)) {
    return inferLiteralType(body as TSESTree.Expression);
  }

  return null;
}

/** Type guard: is the node a TSESTree.Expression? */
function isExpression(node: TSESTree.Node): node is TSESTree.Expression {
  // All expression node types are defined in the AST_NODE_TYPES enum.
  // We check that it is NOT a statement / declaration / other non-expression.
  const statementTypes = new Set<string>([
    AST_NODE_TYPES.BlockStatement,
    AST_NODE_TYPES.BreakStatement,
    AST_NODE_TYPES.ClassDeclaration,
    AST_NODE_TYPES.ContinueStatement,
    AST_NODE_TYPES.DebuggerStatement,
    AST_NODE_TYPES.DoWhileStatement,
    AST_NODE_TYPES.EmptyStatement,
    AST_NODE_TYPES.ExpressionStatement,
    AST_NODE_TYPES.ForInStatement,
    AST_NODE_TYPES.ForOfStatement,
    AST_NODE_TYPES.ForStatement,
    AST_NODE_TYPES.FunctionDeclaration,
    AST_NODE_TYPES.IfStatement,
    AST_NODE_TYPES.LabeledStatement,
    AST_NODE_TYPES.ReturnStatement,
    AST_NODE_TYPES.SwitchStatement,
    AST_NODE_TYPES.ThrowStatement,
    AST_NODE_TYPES.TryStatement,
    AST_NODE_TYPES.TSTypeAnnotation,
    AST_NODE_TYPES.VariableDeclaration,
    AST_NODE_TYPES.WhileStatement,
    AST_NODE_TYPES.WithStatement,
  ]);
  return !statementTypes.has(node.type);
}

// ---------------------------------------------------------------------------
// Fix helpers
// ---------------------------------------------------------------------------

/** Produces the text for a return-type annotation: `: ReturnType`. */
function returnTypeAnnotationText(type: string): string {
  return `: ${type}`;
}

/** Produces the text for a variable type annotation: `: Type`. */
function variableTypeAnnotationText(type: string): string {
  return `: ${type}`;
}

// ---------------------------------------------------------------------------
// Rule implementation
// ---------------------------------------------------------------------------

export const requireExplicitTypes = createRule<[RuleOptions], MessageIds>({
  name: "require-explicit-types",

  meta: {
    type: "problem",
    fixable: "code",
    docs: {
      description:
        "Require explicit type annotations on all exported symbols to enable TypeScript isolated declarations mode.",
    },
    messages: {
      missingReturnType:
        "Exported function '{{name}}' is missing an explicit return type annotation. " +
        "Explicit return types are required for isolatedDeclarations. " +
        "Add `: ReturnType` before the function body.",
      missingVariableType:
        "Exported variable '{{name}}' is missing an explicit type annotation. " +
        "Explicit types are required for isolatedDeclarations. " +
        "Add `: Type` after the variable name.",
      missingParameterType:
        "Parameter '{{name}}' in exported function '{{fnName}}' is missing an explicit type annotation. " +
        "All parameters of exported functions must be typed for isolatedDeclarations.",
      missingPropertyType:
        "Class property '{{name}}' in exported class '{{className}}' is missing an explicit type annotation.",
      addReturnType:
        "Add inferred return type ': {{type}}' (auto-fix available)",
      addVariableType:
        "Add inferred type ': {{type}}' (auto-fix available)",
      cannotInferType:
        "Cannot infer the type automatically. Please add an explicit type annotation manually.",
    },
    hasSuggestions: true,
    schema: [
      {
        type: "object",
        properties: {
          ignoreDefaultExports: {
            type: "boolean",
          },
        },
        additionalProperties: false,
      },
    ],
  },

  defaultOptions: [{ ignoreDefaultExports: false }],

  create(
    context: RuleContext<MessageIds, [RuleOptions]>
  ): RuleListener {
    const options = context.options[0] ?? {};
    const ignoreDefaultExports = options.ignoreDefaultExports ?? false;

    // -----------------------------------------------------------------------
    // Helpers that use the ESLint context
    // -----------------------------------------------------------------------

    /** Reports a missing return-type on a function node and optionally fixes.
     *
     *  Return type annotation placement:
     *
     *  - Regular function:  `function foo(a, b) {`
     *    Insert `: Type` after the closing `)` of the parameter list, i.e.
     *    immediately before `{`.
     *
     *  - Arrow function:    `(a, b) => expr`
     *    Insert `: Type` after the closing `)` of the parameter list, i.e.
     *    immediately before `=>`.  So the fixed form is `(a, b): Type => expr`.
     *
     *  In both cases the anchor is the `)` that closes the parameter list.
     *  For a parameterless arrow `() => expr` the `)` is still present.
     *  We find it by walking backwards from the body: for arrows we look for
     *  the `=>` token then take the token before that (which is `)`).  For
     *  regular functions the token before `{` is `)`.
     */
    function reportMissingReturnType(
      node:
        | TSESTree.FunctionDeclaration
        | TSESTree.FunctionExpression
        | TSESTree.ArrowFunctionExpression,
      name: string
    ): void {
      // Already annotated — nothing to do.
      if (node.returnType !== undefined && node.returnType !== null) return;

      const body = node.body;
      if (body === null || body === undefined) return;

      const inferredType = inferReturnType(
        body as TSESTree.BlockStatement | TSESTree.Expression
      );

      const sourceCode = context.sourceCode;

      // Find the closing `)` of the parameter list.
      // For arrow functions the token sequence before the body is: `)` `=>`
      // For regular functions it is:                               `)` `{`
      // We find it differently per node type to be reliable.
      let closingParen: ReturnType<typeof sourceCode.getTokenBefore>;

      if (node.type === AST_NODE_TYPES.ArrowFunctionExpression) {
        // Walk back from body to find `=>` then take the token before that.
        const arrowToken = sourceCode.getTokenBefore(body, {
          filter: (t) => t.type === "Punctuator" && t.value === "=>",
        });
        if (!arrowToken) return;
        closingParen = sourceCode.getTokenBefore(arrowToken);
      } else {
        // Regular function or method: token before `{` is `)`.
        closingParen = sourceCode.getTokenBefore(body);
      }

      if (!closingParen) return;

      if (inferredType !== null) {
        context.report({
          node,
          messageId: "missingReturnType",
          data: { name },
          fix(fixer) {
            return fixer.insertTextAfter(
              // insertTextAfter inserts immediately after the token's range end.
              closingParen!,
              returnTypeAnnotationText(inferredType)
            );
          },
        });
      } else {
        context.report({
          node,
          messageId: "missingReturnType",
          data: { name },
          suggest: [
            {
              messageId: "cannotInferType",
              fix() {
                // No automatic fix available.
                return null;
              },
            },
          ],
        });
      }
    }

    /** Reports a missing type annotation on a variable declarator. */
    function reportMissingVariableType(
      declarator: TSESTree.VariableDeclarator,
      varName: string
    ): void {
      // Already annotated?
      if (declarator.id.typeAnnotation !== undefined) return;

      const init = declarator.init;
      const inferredType = init !== null && init !== undefined
        ? inferLiteralType(init)
        : null;

      // The type annotation goes between the variable name and `=`.
      // We insert after the `id` identifier token.
      const idNode = declarator.id;

      if (inferredType !== null) {
        context.report({
          node: declarator,
          messageId: "missingVariableType",
          data: { name: varName },
          fix(fixer) {
            return fixer.insertTextAfter(
              idNode,
              variableTypeAnnotationText(inferredType)
            );
          },
        });
      } else {
        context.report({
          node: declarator,
          messageId: "missingVariableType",
          data: { name: varName },
          suggest: [
            {
              messageId: "cannotInferType",
              fix() {
                return null;
              },
            },
          ],
        });
      }
    }

    /** Reports untyped parameters for an exported function. */
    function checkFunctionParams(
      params: TSESTree.Parameter[],
      fnName: string
    ): void {
      for (const param of params) {
        // RestElement, AssignmentPattern, etc. may wrap the identifier.
        const paramNode = unwrapParam(param);
        if (paramNode === null) continue;
        if (paramNode.typeAnnotation === undefined) {
          const paramName = getParamName(param);
          context.report({
            node: param,
            messageId: "missingParameterType",
            data: { name: paramName, fnName },
          });
        }
      }
    }

    // -----------------------------------------------------------------------
    // Visitor methods
    // -----------------------------------------------------------------------

    return {
      // ---- Exported function declarations: `export function foo() {}` ------
      "ExportNamedDeclaration > FunctionDeclaration"(
        node: TSESTree.FunctionDeclaration
      ): void {
        const name =
          node.id?.name ?? "<anonymous>";
        reportMissingReturnType(node, name);
        checkFunctionParams(node.params, name);
      },

      // ---- Exported default function: `export default function() {}` -------
      "ExportDefaultDeclaration > FunctionDeclaration"(
        node: TSESTree.FunctionDeclaration
      ): void {
        if (ignoreDefaultExports) return;
        const name = node.id?.name ?? "default";
        reportMissingReturnType(node, name);
        checkFunctionParams(node.params, name);
      },

      // ---- Exported variable declarations ----------------------------------
      // Covers: `export const x = ...`, `export let x = ...`
      ExportNamedDeclaration(node: TSESTree.ExportNamedDeclaration): void {
        const decl = node.declaration;
        if (decl === null || decl === undefined) return;

        // Variable declarations: `export const foo = 1`
        if (decl.type === AST_NODE_TYPES.VariableDeclaration) {
          for (const declarator of decl.declarations) {
            const init = declarator.init;
            if (init === null || init === undefined) {
              // `export declare const x` — no init, skip
              continue;
            }

            // If the initializer is an arrow function or function expression,
            // we check the function itself rather than the variable type.
            if (
              init.type === AST_NODE_TYPES.ArrowFunctionExpression ||
              init.type === AST_NODE_TYPES.FunctionExpression
            ) {
              const varName = getDeclaratorName(declarator);
              // Check for return type on the function itself.
              reportMissingReturnType(
                init as
                  | TSESTree.ArrowFunctionExpression
                  | TSESTree.FunctionExpression,
                varName
              );
              checkFunctionParams(
                (
                  init as
                    | TSESTree.ArrowFunctionExpression
                    | TSESTree.FunctionExpression
                ).params,
                varName
              );
            } else {
              // Non-function initializer: the variable itself needs a type.
              const varName = getDeclaratorName(declarator);
              reportMissingVariableType(declarator, varName);
            }
          }
        }

        // Class declarations: `export class Foo { ... }`
        if (decl.type === AST_NODE_TYPES.ClassDeclaration) {
          checkExportedClass(decl);
        }
      },

      // ---- Exported default arrow/class: `export default () => ...` --------
      ExportDefaultDeclaration(
        node: TSESTree.ExportDefaultDeclaration
      ): void {
        if (ignoreDefaultExports) return;
        const decl = node.declaration;

        if (
          decl.type === AST_NODE_TYPES.ArrowFunctionExpression ||
          decl.type === AST_NODE_TYPES.FunctionExpression
        ) {
          reportMissingReturnType(
            decl as
              | TSESTree.ArrowFunctionExpression
              | TSESTree.FunctionExpression,
            "default"
          );
          checkFunctionParams(
            (
              decl as
                | TSESTree.ArrowFunctionExpression
                | TSESTree.FunctionExpression
            ).params,
            "default"
          );
        }

        if (decl.type === AST_NODE_TYPES.ClassDeclaration) {
          checkExportedClass(decl as TSESTree.ClassDeclaration);
        }
      },
    };

    // -----------------------------------------------------------------------
    // Class checking
    // -----------------------------------------------------------------------

    function checkExportedClass(node: TSESTree.ClassDeclaration): void {
      const className = node.id?.name ?? "<anonymous>";

      for (const member of node.body.body) {
        // Method definitions: constructor, regular methods
        if (member.type === AST_NODE_TYPES.MethodDefinition) {
          const method = member as TSESTree.MethodDefinition;
          // Skip constructor — its "return type" is the class itself, implicit.
          if (method.kind === "constructor") continue;
          // Skip computed keys (e.g. `[Symbol.iterator]`) — too complex.
          if (method.computed) continue;

          const fn = method.value as TSESTree.FunctionExpression;
          if (fn.returnType !== undefined && fn.returnType !== null) continue;

          const methodName = getKeyName(method.key) ?? "<computed>";
          const inferredType = fn.body !== null
            ? inferReturnType(fn.body)
            : null;

          const insertToken = context.sourceCode.getTokenBefore(fn.body);
          if (!insertToken) continue;

          if (inferredType !== null) {
            context.report({
              node: method,
              messageId: "missingReturnType",
              data: { name: `${className}.${methodName}` },
              fix(fixer) {
                return fixer.insertTextAfter(
                  insertToken,
                  returnTypeAnnotationText(inferredType)
                );
              },
            });
          } else {
            context.report({
              node: method,
              messageId: "missingReturnType",
              data: { name: `${className}.${methodName}` },
              suggest: [
                {
                  messageId: "cannotInferType",
                  fix() {
                    return null;
                  },
                },
              ],
            });
          }

          checkFunctionParams(fn.params, `${className}.${methodName}`);
        }

        // Property definitions: `foo = 1;`, `foo: string = ...`
        if (member.type === AST_NODE_TYPES.PropertyDefinition) {
          const prop = member as TSESTree.PropertyDefinition;
          if (prop.computed) continue;
          if (prop.typeAnnotation !== undefined) continue;
          // Static and instance properties both need types.
          const propName = getKeyName(prop.key) ?? "<computed>";
          context.report({
            node: prop,
            messageId: "missingPropertyType",
            data: { name: propName, className },
          });
        }
      }
    }
  },
});

// ---------------------------------------------------------------------------
// Utility functions
// ---------------------------------------------------------------------------

/** Extracts the name from a VariableDeclarator's id. Handles simple
 *  identifiers; returns `"<destructured>"` for patterns. */
function getDeclaratorName(declarator: TSESTree.VariableDeclarator): string {
  if (declarator.id.type === AST_NODE_TYPES.Identifier) {
    return (declarator.id as TSESTree.Identifier).name;
  }
  return "<destructured>";
}

/** Returns the inner identifier-like node for a function parameter that we
 *  can check for a type annotation, or null when the parameter structure is
 *  too complex (e.g. nested destructuring). */
function unwrapParam(
  param: TSESTree.Parameter
): TSESTree.Identifier | null {
  switch (param.type) {
    case AST_NODE_TYPES.Identifier:
      return param as TSESTree.Identifier;
    case AST_NODE_TYPES.AssignmentPattern: {
      const ap = param as TSESTree.AssignmentPattern;
      if (ap.left.type === AST_NODE_TYPES.Identifier) {
        return ap.left as TSESTree.Identifier;
      }
      return null;
    }
    case AST_NODE_TYPES.RestElement: {
      const re = param as TSESTree.RestElement;
      if (re.argument.type === AST_NODE_TYPES.Identifier) {
        return re.argument as TSESTree.Identifier;
      }
      return null;
    }
    default:
      return null;
  }
}

/** Returns the human-readable name of a function parameter. */
function getParamName(param: TSESTree.Parameter): string {
  switch (param.type) {
    case AST_NODE_TYPES.Identifier:
      return (param as TSESTree.Identifier).name;
    case AST_NODE_TYPES.AssignmentPattern: {
      const ap = param as TSESTree.AssignmentPattern;
      if (ap.left.type === AST_NODE_TYPES.Identifier) {
        return (ap.left as TSESTree.Identifier).name;
      }
      return "<pattern>";
    }
    case AST_NODE_TYPES.RestElement: {
      const re = param as TSESTree.RestElement;
      if (re.argument.type === AST_NODE_TYPES.Identifier) {
        return `...${(re.argument as TSESTree.Identifier).name}`;
      }
      return "...rest";
    }
    default:
      return "<pattern>";
  }
}

/** Returns the string key name for a method/property key node. Returns null
 *  for computed keys. */
function getKeyName(key: TSESTree.Expression | TSESTree.PrivateIdentifier): string | null {
  if (key.type === AST_NODE_TYPES.Identifier) {
    return (key as TSESTree.Identifier).name;
  }
  if (key.type === AST_NODE_TYPES.PrivateIdentifier) {
    return `#${(key as TSESTree.PrivateIdentifier).name}`;
  }
  if (key.type === AST_NODE_TYPES.Literal) {
    return String((key as TSESTree.Literal).value);
  }
  return null;
}
