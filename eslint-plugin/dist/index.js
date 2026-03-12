import { ESLintUtils, AST_NODE_TYPES } from '@typescript-eslint/utils';

// src/rules/require-explicit-types.ts

// src/utils.ts
function hasReturnTypeAnnotation(node) {
  return node.returnType != null;
}
function hasTypeAnnotation(declarator) {
  if (declarator.id.typeAnnotation != null) {
    return true;
  }
  const init = declarator.init;
  if (init == null) {
    return false;
  }
  if (init.type === "ArrowFunctionExpression" || init.type === "FunctionExpression") {
    return init.returnType != null;
  }
  return false;
}
function isOverloadSignature(node) {
  return node.body == null;
}

// src/rules/require-explicit-types.ts
var createRule = ESLintUtils.RuleCreator(
  (name) => `https://github.com/nicholasgasior/rules_typescript/blob/main/eslint-plugin/docs/rules/${name}.md`
);
var requireExplicitTypes = createRule({
  name: "require-explicit-types",
  meta: {
    type: "problem",
    docs: {
      description: "Require explicit type annotations on exported bindings for isolated declarations compatibility"
    },
    messages: {
      missingFunctionReturnType: "Exported function '{{name}}' is missing an explicit return type annotation. Add a return type (e.g. `function {{name}}(): ReturnType`) to enable isolated declarations emit without a type-inference pass. See: https://www.typescriptlang.org/tsconfig#isolatedDeclarations",
      missingVariableType: "Exported variable '{{name}}' is missing an explicit type annotation. Add a type annotation (e.g. `const {{name}}: SomeType = ...`) so that the .d.ts can be emitted without type inference. See: https://www.typescriptlang.org/tsconfig#isolatedDeclarations",
      missingDefaultExportType: "Default export is missing an explicit type annotation. Wrap in a typed variable (`const value: Type = ...; export default value`) or add a return-type annotation to the function. See: https://www.typescriptlang.org/tsconfig#isolatedDeclarations"
    },
    schema: []
  },
  defaultOptions: [],
  create(context) {
    function checkExportNamedDeclaration(node) {
      const { declaration } = node;
      if (declaration == null) {
        return;
      }
      if (node.exportKind === "type") {
        return;
      }
      if (declaration.type === AST_NODE_TYPES.FunctionDeclaration) {
        if (isOverloadSignature(declaration)) {
          return;
        }
        if (!hasReturnTypeAnnotation(declaration)) {
          const funcName = declaration.id?.name ?? "<anonymous>";
          context.report({
            node: declaration,
            messageId: "missingFunctionReturnType",
            data: { name: funcName }
          });
        }
        return;
      }
      if (declaration.type === AST_NODE_TYPES.TSDeclareFunction) {
        return;
      }
      if (declaration.type === AST_NODE_TYPES.VariableDeclaration) {
        if (declaration.declare === true) {
          return;
        }
        for (const declarator of declaration.declarations) {
          const init = declarator.init;
          const bindingName = getBindingName(declarator.id);
          if (init != null && (init.type === AST_NODE_TYPES.ArrowFunctionExpression || init.type === AST_NODE_TYPES.FunctionExpression)) {
            const hasBindingType = declarator.id.typeAnnotation != null;
            const hasFunctionReturnType = hasReturnTypeAnnotation(init);
            if (!hasBindingType && !hasFunctionReturnType) {
              context.report({
                node: declarator,
                messageId: "missingFunctionReturnType",
                data: { name: bindingName }
              });
            }
            continue;
          }
          if (!hasTypeAnnotation(declarator)) {
            context.report({
              node: declarator,
              messageId: "missingVariableType",
              data: { name: bindingName }
            });
          }
        }
        return;
      }
    }
    function checkExportDefaultDeclaration(node) {
      const { declaration } = node;
      if (declaration.type === AST_NODE_TYPES.FunctionDeclaration) {
        if (isOverloadSignature(declaration)) {
          return;
        }
        if (!hasReturnTypeAnnotation(declaration)) {
          const funcName = declaration.id?.name ?? "default";
          context.report({
            node: declaration,
            messageId: "missingFunctionReturnType",
            data: { name: funcName }
          });
        }
        return;
      }
      if (declaration.type === AST_NODE_TYPES.ArrowFunctionExpression) {
        if (!hasReturnTypeAnnotation(declaration)) {
          context.report({
            node: declaration,
            messageId: "missingDefaultExportType"
          });
        }
        return;
      }
      if (declaration.type === AST_NODE_TYPES.Identifier) {
        return;
      }
      if (declaration.type === AST_NODE_TYPES.Literal || declaration.type === AST_NODE_TYPES.TemplateLiteral) {
        return;
      }
      if (declaration.type === AST_NODE_TYPES.ClassDeclaration) {
        return;
      }
      if (declaration.type === AST_NODE_TYPES.TSDeclareFunction || declaration.type === AST_NODE_TYPES.TSInterfaceDeclaration || declaration.type === AST_NODE_TYPES.TSTypeAliasDeclaration || declaration.type === AST_NODE_TYPES.TSEnumDeclaration || declaration.type === AST_NODE_TYPES.TSModuleDeclaration) {
        return;
      }
      context.report({
        node: declaration,
        messageId: "missingDefaultExportType"
      });
    }
    return {
      ExportNamedDeclaration: checkExportNamedDeclaration,
      ExportDefaultDeclaration: checkExportDefaultDeclaration
    };
  }
});
function getBindingName(node) {
  if (node.type === AST_NODE_TYPES.Identifier) {
    return node.name;
  }
  return "<destructured>";
}

// src/index.ts
var plugin = {
  meta: {
    name: "@rules_typescript/eslint-plugin-isolated-declarations",
    version: "0.1.0"
  },
  rules: {
    "require-explicit-types": requireExplicitTypes
  },
  /**
   * Recommended configuration for ESLint flat config (ESLint 9+).
   *
   * Enables all rules at "error" severity.  This is intentionally strict:
   * isolated declarations is all-or-nothing per package.  Use the gradual
   * rollout approach (see README) to adopt incrementally.
   */
  configs: {}
};
plugin.configs["recommended"] = {
  plugins: {
    "isolated-declarations": plugin
  },
  rules: {
    "isolated-declarations/require-explicit-types": "error"
  }
};
var index_default = plugin;

export { index_default as default, plugin, requireExplicitTypes };
//# sourceMappingURL=index.js.map
//# sourceMappingURL=index.js.map