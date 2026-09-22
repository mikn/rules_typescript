import { existsSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

export function workspaceRoot(from: string): string {
  let dir = dirname(fileURLToPath(from));
  while (!existsSync(join(dir, "MODULE.bazel"))) {
    const parent = dirname(dir);
    if (parent === dir) throw new Error(`no MODULE.bazel above ${from}`);
    dir = parent;
  }
  return dir;
}
