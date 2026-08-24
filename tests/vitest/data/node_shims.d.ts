// Just enough of Node to read a fixture: the generated tsconfig has no `types`
// knob, so @types/node cannot be brought in (GAP-02).
declare module "node:fs" {
  export function readFileSync(path: string, encoding: "utf8"): string;
}

declare const process: { env: Record<string, string | undefined> };
