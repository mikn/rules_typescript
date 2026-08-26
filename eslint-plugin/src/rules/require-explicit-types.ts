/**
 * ESLint rule: `isolated-declarations/require-explicit-types`
 *
 * Reports exported bindings, parameters and class members that lack explicit
 * type annotations, which is what TypeScript's `isolatedDeclarations` mode
 * requires in order to emit a `.d.ts` per file without a type-inference pass.
 *
 * Types that are readable straight off the AST (literals, uniform array
 * literals, single-`return` bodies) come with an auto-fix; everything else is
 * reported with a suggestion telling the developer to annotate by hand.
 */

import { AST_NODE_TYPES, ESLintUtils } from '@typescript-eslint/utils';
import type { TSESTree } from '@typescript-eslint/utils';
import type {
  RuleContext,
  RuleListener,
} from '@typescript-eslint/utils/ts-eslint';
import {
  hasBindingTypeAnnotation,
  hasReturnTypeAnnotation,
  hasTypeAnnotation,
} from '../utils.js';

type MessageIds =
  | 'missingReturnType'
  | 'missingVariableType'
  | 'missingParameterType'
  | 'missingPropertyType'
  | 'missingDefaultExportType'
  | 'cannotInferType';

export interface RuleOptions {
  /** Skip every `export default` form. Defaults to `false`. */
  ignoreDefaultExports?: boolean;
}

function inferLiteralType(node: TSESTree.Expression): string | null {
  switch (node.type) {
    case AST_NODE_TYPES.Literal: {
      if (typeof node.value === 'string') return 'string';
      if (typeof node.value === 'number') return 'number';
      if (typeof node.value === 'boolean') return 'boolean';
      if (node.value === null) return 'null';
      if ('bigint' in node && node.bigint !== undefined) return 'bigint';
      return null;
    }

    case AST_NODE_TYPES.TemplateLiteral:
      return 'string';

    case AST_NODE_TYPES.UnaryExpression: {
      if (node.operator === 'void') return 'undefined';
      if (
        (node.operator === '-' || node.operator === '+') &&
        node.argument.type === AST_NODE_TYPES.Literal &&
        typeof node.argument.value === 'number'
      ) {
        return 'number';
      }
      if (
        node.operator === '!' &&
        node.argument.type === AST_NODE_TYPES.Literal
      ) {
        return 'boolean';
      }
      return null;
    }

    case AST_NODE_TYPES.Identifier: {
      if (node.name === 'undefined') return 'undefined';
      if (node.name === 'NaN' || node.name === 'Infinity') return 'number';
      return null;
    }

    case AST_NODE_TYPES.ArrayExpression: {
      if (node.elements.length === 0) return 'never[]';
      const elementTypes = new Set<string>();
      for (const el of node.elements) {
        if (el === null || el.type === AST_NODE_TYPES.SpreadElement) {
          return null;
        }
        const elType = inferLiteralType(el);
        if (elType === null) return null;
        elementTypes.add(elType);
      }
      if (elementTypes.size === 1) {
        const [elType] = [...elementTypes];
        return `${elType}[]`;
      }
      if (elementTypes.size <= 4) {
        return `(${[...elementTypes].sort().join(' | ')})[]`;
      }
      return null;
    }

    case AST_NODE_TYPES.ObjectExpression:
      // A structural annotation synthesised from an object literal would be
      // verbose and would silently lose optionality and method signatures.
      return null;

    default:
      return null;
  }
}

function inferReturnType(
  body: TSESTree.BlockStatement | TSESTree.Expression,
): string | null {
  if (body.type === AST_NODE_TYPES.BlockStatement) {
    if (body.body.length !== 1) return null;
    const stmt = body.body[0];
    if (stmt === undefined || stmt.type !== AST_NODE_TYPES.ReturnStatement) {
      return null;
    }
    if (stmt.argument == null) return 'void';
    return inferLiteralType(stmt.argument);
  }

  return inferLiteralType(body);
}

function getDeclaratorName(declarator: TSESTree.VariableDeclarator): string {
  if (declarator.id.type === AST_NODE_TYPES.Identifier) {
    return declarator.id.name;
  }
  return '<destructured>';
}

function unwrapParam(param: TSESTree.Parameter): TSESTree.Identifier | null {
  switch (param.type) {
    case AST_NODE_TYPES.Identifier:
      return param;
    case AST_NODE_TYPES.AssignmentPattern:
      return param.left.type === AST_NODE_TYPES.Identifier ? param.left : null;
    case AST_NODE_TYPES.RestElement:
      return param.argument.type === AST_NODE_TYPES.Identifier
        ? param.argument
        : null;
    default:
      return null;
  }
}

function getParamName(param: TSESTree.Parameter): string {
  switch (param.type) {
    case AST_NODE_TYPES.Identifier:
      return param.name;
    case AST_NODE_TYPES.AssignmentPattern:
      return param.left.type === AST_NODE_TYPES.Identifier
        ? param.left.name
        : '<pattern>';
    case AST_NODE_TYPES.RestElement:
      return param.argument.type === AST_NODE_TYPES.Identifier
        ? `...${param.argument.name}`
        : '...rest';
    default:
      return '<pattern>';
  }
}

function getKeyName(
  key: TSESTree.Expression | TSESTree.PrivateIdentifier,
): string | null {
  if (key.type === AST_NODE_TYPES.Identifier) {
    return key.name;
  }
  if (key.type === AST_NODE_TYPES.PrivateIdentifier) {
    return `#${key.name}`;
  }
  if (key.type === AST_NODE_TYPES.Literal) {
    return String(key.value);
  }
  return null;
}

/** Default-export forms that already describe themselves in a `.d.ts`. */
const SELF_DESCRIBING_DEFAULT_EXPORTS: ReadonlySet<string> = new Set<string>([
  AST_NODE_TYPES.ClassDeclaration,
  AST_NODE_TYPES.FunctionDeclaration,
  AST_NODE_TYPES.Identifier,
  AST_NODE_TYPES.Literal,
  AST_NODE_TYPES.TemplateLiteral,
  AST_NODE_TYPES.TSDeclareFunction,
  AST_NODE_TYPES.TSEnumDeclaration,
  AST_NODE_TYPES.TSInterfaceDeclaration,
  AST_NODE_TYPES.TSModuleDeclaration,
  AST_NODE_TYPES.TSTypeAliasDeclaration,
]);

const createRule = ESLintUtils.RuleCreator(
  (name) =>
    `https://github.com/mikn/rules_typescript/blob/main/eslint-plugin/docs/rules/${name}.md`,
);

export const requireExplicitTypes = createRule<[RuleOptions], MessageIds>({
  name: 'require-explicit-types',

  meta: {
    type: 'problem',
    fixable: 'code',
    hasSuggestions: true,
    docs: {
      description:
        'Require explicit type annotations on exported bindings for isolated declarations compatibility',
    },
    messages: {
      missingReturnType:
        "Exported function '{{name}}' is missing an explicit return type annotation. " +
        'Add a return type (e.g. `function {{name}}(): ReturnType`) to enable isolated ' +
        'declarations emit without a type-inference pass. ' +
        'See: https://www.typescriptlang.org/tsconfig#isolatedDeclarations',

      missingVariableType:
        "Exported variable '{{name}}' is missing an explicit type annotation. " +
        'Add a type annotation (e.g. `const {{name}}: SomeType = ...`) so that the ' +
        '.d.ts can be emitted without type inference. ' +
        'See: https://www.typescriptlang.org/tsconfig#isolatedDeclarations',

      missingParameterType:
        "Parameter '{{name}}' of exported function '{{fnName}}' is missing an explicit " +
        'type annotation. Every parameter of an exported function must be typed for ' +
        'isolated declarations emit. ' +
        'See: https://www.typescriptlang.org/tsconfig#isolatedDeclarations',

      missingPropertyType:
        "Property '{{name}}' of exported class '{{className}}' is missing an explicit " +
        'type annotation. ' +
        'See: https://www.typescriptlang.org/tsconfig#isolatedDeclarations',

      missingDefaultExportType:
        'Default export is missing an explicit type annotation. ' +
        'Wrap in a typed variable (`const value: Type = ...; export default value`) ' +
        'or add a return-type annotation to the function. ' +
        'See: https://www.typescriptlang.org/tsconfig#isolatedDeclarations',

      cannotInferType:
        'Cannot infer the type automatically. Please add an explicit type annotation manually.',
    },
    schema: [
      {
        type: 'object',
        properties: {
          ignoreDefaultExports: {
            type: 'boolean',
          },
        },
        additionalProperties: false,
      },
    ],
  },

  defaultOptions: [{ ignoreDefaultExports: false }],

  create(context: RuleContext<MessageIds, [RuleOptions]>): RuleListener {
    const ignoreDefaultExports =
      context.options[0]?.ignoreDefaultExports ?? false;

    function reportMissingReturnType(
      node:
        | TSESTree.FunctionDeclaration
        | TSESTree.FunctionExpression
        | TSESTree.ArrowFunctionExpression,
      name: string,
    ): void {
      if (hasReturnTypeAnnotation(node)) return;

      const body = node.body;
      if (body == null) return;

      const inferredType = inferReturnType(body);
      const sourceCode = context.sourceCode;

      // The annotation anchors on the `)` that closes the parameter list, which
      // is the token before `{` for a function and before `=>` for an arrow.
      let closingParen: ReturnType<typeof sourceCode.getTokenBefore>;
      if (node.type === AST_NODE_TYPES.ArrowFunctionExpression) {
        const arrowToken = sourceCode.getTokenBefore(body, {
          filter: (t) => t.type === 'Punctuator' && t.value === '=>',
        });
        if (arrowToken == null) return;
        closingParen = sourceCode.getTokenBefore(arrowToken);
      } else {
        closingParen = sourceCode.getTokenBefore(body);
      }
      if (closingParen == null) return;

      const anchor = closingParen;
      if (inferredType !== null) {
        context.report({
          node,
          messageId: 'missingReturnType',
          data: { name },
          fix: (fixer) => fixer.insertTextAfter(anchor, `: ${inferredType}`),
        });
        return;
      }

      context.report({
        node,
        messageId: 'missingReturnType',
        data: { name },
        suggest: [{ messageId: 'cannotInferType', fix: () => null }],
      });
    }

    function reportMissingVariableType(
      declarator: TSESTree.VariableDeclarator,
      varName: string,
    ): void {
      if (hasTypeAnnotation(declarator)) return;

      const init = declarator.init;
      const inferredType = init == null ? null : inferLiteralType(init);
      const idNode = declarator.id;

      if (inferredType !== null) {
        context.report({
          node: declarator,
          messageId: 'missingVariableType',
          data: { name: varName },
          fix: (fixer) => fixer.insertTextAfter(idNode, `: ${inferredType}`),
        });
        return;
      }

      context.report({
        node: declarator,
        messageId: 'missingVariableType',
        data: { name: varName },
        suggest: [{ messageId: 'cannotInferType', fix: () => null }],
      });
    }

    function checkFunctionParams(
      params: TSESTree.Parameter[],
      fnName: string,
    ): void {
      for (const param of params) {
        const paramNode = unwrapParam(param);
        if (paramNode === null) continue;
        if (paramNode.typeAnnotation == null) {
          context.report({
            node: param,
            messageId: 'missingParameterType',
            data: { name: getParamName(param), fnName },
          });
        }
      }
    }

    function checkExportedClass(node: TSESTree.ClassDeclaration): void {
      const className = node.id?.name ?? '<anonymous>';

      for (const member of node.body.body) {
        if (member.type === AST_NODE_TYPES.MethodDefinition) {
          // A constructor's return type is the class itself.
          if (member.kind === 'constructor') continue;
          if (member.computed) continue;

          const fn = member.value;
          const methodName = getKeyName(member.key) ?? '<computed>';
          // TS1095: annotating a setter's return type is a compile error, so
          // reporting one here would autofix working code into broken code.
          if (member.kind !== 'set' && !hasReturnTypeAnnotation(fn)) {
            const body = fn.body;
            const inferredType = body == null ? null : inferReturnType(body);
            const insertToken =
              body == null ? null : context.sourceCode.getTokenBefore(body);

            if (inferredType !== null && insertToken != null) {
              context.report({
                node: member,
                messageId: 'missingReturnType',
                data: { name: `${className}.${methodName}` },
                fix: (fixer) =>
                  fixer.insertTextAfter(insertToken, `: ${inferredType}`),
              });
            } else {
              context.report({
                node: member,
                messageId: 'missingReturnType',
                data: { name: `${className}.${methodName}` },
                suggest: [{ messageId: 'cannotInferType', fix: () => null }],
              });
            }
          }

          checkFunctionParams(fn.params, `${className}.${methodName}`);
          continue;
        }

        if (member.type === AST_NODE_TYPES.PropertyDefinition) {
          if (member.computed) continue;
          if (member.typeAnnotation != null) continue;
          context.report({
            node: member,
            messageId: 'missingPropertyType',
            data: { name: getKeyName(member.key) ?? '<computed>', className },
          });
        }
      }
    }

    return {
      'ExportNamedDeclaration > FunctionDeclaration'(
        node: TSESTree.FunctionDeclaration,
      ): void {
        const name = node.id?.name ?? '<anonymous>';
        reportMissingReturnType(node, name);
        checkFunctionParams(node.params, name);
      },

      'ExportDefaultDeclaration > FunctionDeclaration'(
        node: TSESTree.FunctionDeclaration,
      ): void {
        if (ignoreDefaultExports) return;
        const name = node.id?.name ?? 'default';
        reportMissingReturnType(node, name);
        checkFunctionParams(node.params, name);
      },

      ExportNamedDeclaration(node: TSESTree.ExportNamedDeclaration): void {
        const decl = node.declaration;
        if (decl == null) return;
        if (node.exportKind === 'type') return;

        if (decl.type === AST_NODE_TYPES.VariableDeclaration) {
          if (decl.declare === true) return;

          for (const declarator of decl.declarations) {
            const init = declarator.init;
            if (init == null) continue;

            const varName = getDeclaratorName(declarator);
            if (
              init.type === AST_NODE_TYPES.ArrowFunctionExpression ||
              init.type === AST_NODE_TYPES.FunctionExpression
            ) {
              // `const fn: () => string = () => 'x'` is already emittable: the
              // binding annotation describes the signature, so annotating the
              // function would rewrite code that is correct as written.
              if (hasBindingTypeAnnotation(declarator)) continue;
              reportMissingReturnType(init, varName);
              checkFunctionParams(init.params, varName);
            } else {
              reportMissingVariableType(declarator, varName);
            }
          }
        }

        if (decl.type === AST_NODE_TYPES.ClassDeclaration) {
          checkExportedClass(decl);
        }
      },

      ExportDefaultDeclaration(node: TSESTree.ExportDefaultDeclaration): void {
        if (ignoreDefaultExports) return;
        const decl = node.declaration;

        if (
          decl.type === AST_NODE_TYPES.ArrowFunctionExpression ||
          decl.type === AST_NODE_TYPES.FunctionExpression
        ) {
          reportMissingReturnType(decl, 'default');
          checkFunctionParams(decl.params, 'default');
          return;
        }

        if (decl.type === AST_NODE_TYPES.ClassDeclaration) {
          checkExportedClass(decl);
          return;
        }

        if (SELF_DESCRIBING_DEFAULT_EXPORTS.has(decl.type)) return;

        context.report({
          node: decl,
          messageId: 'missingDefaultExportType',
        });
      },
    };
  },
});
