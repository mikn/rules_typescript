// The bundler half of css_module: it hands Vite the naming function css_module
// already used, so the class names in the bundle are the ones the .d.ts
// declares rather than a second, independent derivation of them.
//
// The name comes out of the map css_module wrote beside the stylesheet
// (<file>.exports.json), so a css_module attr that salts or reshapes the answer
// -- hash_prefix, locals_convention -- needs no second declaration here. Only
// when there is no such map, which is the dev server serving a source tree, is
// the name recomputed; scopedName is a pure function of the same bytes, so it
// lands on the same answer.
import { readFileSync } from "node:fs";
import { dirname, isAbsolute, resolve } from "node:path";
import { scopedName } from "./scoped_name";

type ExportMap = Record<string, string>;

const SCOPED_TAIL = /_([0-9a-f]{8})$/;

function readExportMap(cssFileName: string): ExportMap | null {
  try {
    return JSON.parse(readFileSync(cssFileName + ".exports.json", "utf8")) as ExportMap;
  } catch {
    return null;
  }
}

// A `composes` value names several classes; the file's own is the first, and the
// rest belong to the files it composed from.
function ownName(exported: string): string {
  return exported.split(/\s+/)[0];
}

function contentHashOf(map: ExportMap): string | null {
  for (const exported of Object.values(map)) {
    const match = SCOPED_TAIL.exec(ownName(exported));
    if (match) return match[1];
  }
  return null;
}

function ownNames(map: ExportMap): Set<string> {
  return new Set(Object.values(map).map(ownName));
}

/** The options that decide the answer css_module already wrote a .d.ts from. */
const RULE_OWNED = {
  hashPrefix: "hash_prefix",
  scopeBehaviour: "scope_behaviour",
  localsConvention: "locals_convention",
  exportGlobals: "export_globals",
} as const;

interface ResolvedCssModules {
  generateScopedName?: unknown;
  [option: string]: unknown;
}

// Identity is the wrong test for "is this ours". A framework plugin that drives
// its own `createBuilder` makes Vite resolve the config once per environment,
// and the plugin factory runs again for each -- so the function on the resolved
// config is a different closure from the one the checking instance closed over,
// though both came from here. The mark travels with the function instead.
const OURS = Symbol.for("rules_typescript.generateScopedName");

function isOurs(fn: unknown): boolean {
  return typeof fn === "function" && (fn as unknown as Record<symbol, unknown>)[OURS] === true;
}

export function cssModulesPlugin() {
  const generateScopedName = (localName: string, filename: string, cssText: string): string => {
    const map = readExportMap(filename);
    if (map === null) return scopedName(localName, cssText);

    const hash = contentHashOf(map);
    const scoped = hash === null ? scopedName(localName, cssText) : `_${localName}_${hash}`;
    if (!ownNames(map).has(scoped)) {
      throw new Error(
        `[rules_typescript] ${filename} defines the local name "${localName}", ` +
          `which css_module's export map does not declare.\n` +
          `  css_module wrote: ${JSON.stringify(map)}\n` +
          `The .d.ts generated from that map is what typechecked, so this build ` +
          `would ship a class name no TypeScript declaration knows about. It means ` +
          `the CSS Modules implementation in this Vite differs from the one ` +
          `css_module compiled with; re-run the build after \`bazel clean\`, and ` +
          `report the two versions if it persists.`,
      );
    }
    return scoped;
  };
  (generateScopedName as unknown as Record<symbol, unknown>)[OURS] = true;

  return {
    name: "rules-typescript:css-modules",
    enforce: "pre" as const,

    config() {
      return { css: { modules: { generateScopedName } } };
    },

    // The merge is what decides which generateScopedName Vite ends up with, and
    // a plugin declared in a vite_config wins it. Left undetected, the bundle
    // would carry names the .d.ts never declared.
    configResolved(config: { css?: { modules?: ResolvedCssModules | false } }) {
      const modules = config.css?.modules;
      if (modules === false) {
        throw new Error(
          "[rules_typescript] this build sets css.modules = false, which makes Vite " +
            "treat a *.module.css as a plain stylesheet and give the importer no " +
            "class-name object at all.\n" +
            "The .d.ts a css_module target generated promises that object, so remove " +
            "the setting from the vite_config, or stop routing the stylesheet " +
            "through css_module.",
        );
      }
      if (!modules || !isOurs(modules.generateScopedName)) {
        throw new Error(
          "[rules_typescript] something in this build sets css.modules." +
            "generateScopedName, which decides the class names css_module already " +
            "generated a .d.ts from.\n" +
            "Remove it from the vite_config: the names are the ruleset's to mint, " +
            "and hash_prefix on the css_module target is the supported way to " +
            "change them.",
        );
      }
      for (const [option, attr] of Object.entries(RULE_OWNED)) {
        if (modules[option] === undefined) continue;
        throw new Error(
          `[rules_typescript] this build sets css.modules.${option}, which changes ` +
            `the export map css_module wrote the .d.ts from.\n` +
            `Set it as \`${attr}\` on the css_module target instead, where the rule ` +
            `that generates the .d.ts can see it.`,
        );
      }
    },
  };
}

// The vitest half. A `.module.css` is not loadable by Node, so the import is
// answered with the map css_module wrote, which is what lets a test assert on a
// rendered class attribute.
export function cssModulesTestPlugin() {
  const PREFIX = "\0css-module:";
  return {
    name: "rules-typescript:css-modules-test",
    enforce: "pre" as const,
    // The resolved id must not itself look like a CSS request: vite's own css
    // plugins key off the .css suffix and would transform what `load` returns.
    resolveId(id: string, importer?: string): string | null {
      const bare = id.split("?")[0];
      if (!bare.endsWith(".module.css")) return null;
      const abs = isAbsolute(bare) || !importer ? bare : resolve(dirname(importer), bare);
      return PREFIX + abs + ".mjs";
    },
    load(id: string): string | null {
      if (!id.startsWith(PREFIX)) return null;
      const cssFileName = id.slice(PREFIX.length, -".mjs".length);
      const map = readExportMap(cssFileName);
      // No map means no css_module target compiled this stylesheet, so there is
      // no .d.ts to agree with either; the property name keeps such a test
      // running rather than failing on an unloadable import.
      if (map === null) {
        return 'export default new Proxy({}, { get: (_, k) => typeof k === "string" ? k : undefined });';
      }
      return `export default ${JSON.stringify(map)};`;
    },
  };
}
