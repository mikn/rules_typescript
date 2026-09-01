import type { Program } from "estree";

export function isProgram(node: { type: string }): node is Program {
  return node.type === "Program";
}
