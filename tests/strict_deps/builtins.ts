import { readFileSync } from "node:fs";
import { join } from "path";

export function readSibling(dir: string, name: string): string {
  return readFileSync(join(dir, name), "utf8");
}
