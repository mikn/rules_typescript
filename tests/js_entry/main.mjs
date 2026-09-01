import { existsSync } from "node:fs";
import { writeFile } from "node:fs/promises";
import { greet } from "./greet.mjs";

const args = process.argv.slice(2);
const out = args[0] === "--out" ? args[1] : "";

if (!out) {
  process.stdout.write(greet("runfiles") + "\n");
} else {
  const nodeModules = process.env.TS_CODEGEN_NODE_MODULES ?? "";
  await writeFile(
    out,
    [
      `export const greeting: string = ${JSON.stringify(greet("codegen"))};`,
      `export const nodeBinary: boolean = ${Boolean(process.env.NODE_BINARY)};`,
      `export const nodeModulesDir: boolean = ${Boolean(nodeModules) && existsSync(nodeModules)};`,
      "",
    ].join("\n"),
  );
}
