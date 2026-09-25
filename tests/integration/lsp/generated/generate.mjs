import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";

const [, , input, output, mode] = process.argv;
const names = JSON.parse(readFileSync(input, "utf8"));
if (mode === "tree") {
  mkdirSync(output, { recursive: true });
  for (const name of names) {
    writeFileSync(
      join(output, `${name}.d.ts`),
      `export declare const ${name}: string;\nexport declare const generatedTreeMembers: { ${names.map((member) => `readonly ${JSON.stringify(member)}: string;`).join(" ")} };\n`,
    );
  }
} else {
  mkdirSync(dirname(output), { recursive: true });
  writeFileSync(
    output,
    `export const generatedValue: string = ${JSON.stringify(names.join(","))};\nexport const generatedMembers = ${JSON.stringify(Object.fromEntries(names.map((name) => [name, name])))} as const;\n`,
  );
}
