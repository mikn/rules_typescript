import type { ModuleJSON } from "rollup";

export function declarationCount(module: ModuleJSON): number {
  return module.ast.body.filter((node) => node.type === "VariableDeclaration").length;
}
