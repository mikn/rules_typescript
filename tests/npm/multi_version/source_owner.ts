import { createRequire } from "node:module";
import { minimatch, type MinimatchOptions } from "minimatch";

export const ownerHasBraceExpandMax: "braceExpandMax" extends keyof MinimatchOptions ? true : false = false;

export function sourceOwnerVersion(): string {
  if (!minimatch("a", "*")) throw new Error("minimatch did not load");
  return createRequire(import.meta.url)("minimatch/package.json").version;
}
