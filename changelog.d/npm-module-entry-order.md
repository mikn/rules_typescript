### Fixed

- **A bare import of an npm package resolves to the file TypeScript resolves it
  to, and a `compilerOptions.types` entry to the declaration.** One file
  answered both: `npm_import` read `exports`, `typings` and `types` for a
  declaration and fell back to `index.d.ts`, and `ts_compile` pinned the
  package's `paths` key to it. TypeScript's order for a bare specifier is
  `exports`, `typings`/`types`, `main`, then `index.ts`, `index.tsx`,
  `index.d.ts`, so `@cloudflare/workers-types`, which names nothing and ships
  `index.ts`, a module, beside `index.d.ts`, a global script, was pinned to the
  script: `import type { TraceItem } from "@cloudflare/workers-types"` was
  `TS2306: File '.../index.d.ts' is not a module`. Each package now carries a
  module entry (`NpmPackageInfo.module_entry_file`, the generated stanza's
  `module_entry`), read in that order with `.ts` and `.tsx` taken ahead of the
  `.d.ts` beside a `.js` target, and `paths` is pinned to it in the build's
  tsconfig and the editor's; a `.ts` entry is staged with the declarations and,
  under `node_modules/<name>/`, type-checked and never emitted. The declaration
  entry (`exports_types_file`) keeps `compilerOptions.types` and reference
  directives, declarations only, and reads `main` as
  `resolveTypeReferenceDirective` does: `postcss-value-parser` (`main:
  lib/index.js`, no `types`) designates `lib/index.d.ts` where it designated
  nothing.
