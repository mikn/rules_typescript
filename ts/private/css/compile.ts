// Compiles one CSS Module with postcss-modules -- the same library Vite
// bundles -- and writes the export map it produced plus the .d.ts derived from
// that map's keys. Two outputs, one map, so they cannot disagree about which
// names exist.
//
//   node css_module_compile.mjs <in.module.css> <out.exports.json> <out.d.ts> <optionsJson>
import { readFileSync, writeFileSync } from "node:fs";
import postcss from "postcss";
import cssModules from "postcss-modules";
import { scopedName } from "./scoped_name";

interface CompileOptions {
  localsConvention?: "camelCase" | "camelCaseOnly" | "dashes" | "dashesOnly" | "all" | "none";
  scopeBehaviour?: "local" | "global";
  hashPrefix?: string;
  exportGlobals?: boolean;
}

const [, , cssPath, exportsPath, dtsPath, optionsJson] = process.argv;
const options: CompileOptions = JSON.parse(optionsJson || "{}");
const hashPrefix = options.hashPrefix ?? "";

let exportTokens: Record<string, string> = {};

try {
  await postcss([
    cssModules({
      getJSON(_cssFileName: string, json: Record<string, string>) {
        exportTokens = json;
      },
      // The name in the .d.ts is the name in the browser only because this is
      // the one function that mints it. Nothing here reads the filename.
      generateScopedName: (name: string, _filename: string, cssText: string) =>
        scopedName(name, cssText, hashPrefix),
      localsConvention: options.localsConvention,
      scopeBehaviour: options.scopeBehaviour,
      exportGlobals: options.exportGlobals,
    }),
  ]).process(readFileSync(cssPath, "utf8"), { from: cssPath });
} catch (err) {
  process.stderr.write(
    `css_module: postcss-modules rejected ${cssPath}\n  ` +
      `${err instanceof Error ? err.message : String(err)}\n`,
  );
  process.exit(1);
}

writeFileSync(exportsPath, JSON.stringify(exportTokens, null, 2) + "\n", "utf8");

const isBareIdent = (name: string) => /^[A-Za-z_$][A-Za-z0-9_$]*$/.test(name);
const fields = Object.keys(exportTokens).map(
  (name) => `  readonly ${isBareIdent(name) ? name : JSON.stringify(name)}: string;`,
);
writeFileSync(
  dtsPath,
  ["declare const styles: {", ...fields, "};", "export default styles;", ""].join("\n"),
  "utf8",
);
