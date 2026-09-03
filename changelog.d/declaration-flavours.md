### Fixed

- **A checked-in `.d.mts` or `.d.cts` is admitted wherever a `.d.ts` is.**
  `ts_compile` already classified one as a declaration and emitted one for a
  `.mjs` / `.cjs` src, but `srcs`, `public_globals` and `types_srcs` refused
  the extension. `import { compile } from "./compile.mjs"` beside a
  hand-written `compile.d.mts`, the pairing `tsc` resolves by name, failed as
  `TS2307` with no attribute to put the declaration in. It is passed through to
  consumers the way a `.d.ts` is, a script-mode one declares globals the way a
  `.d.ts` does, and a relative `types` entry may name one. TypeScript resolves
  the `.mjs` specifier to it ahead of the `.mjs` itself, so a checked-in
  declaration types an untyped JavaScript module whether or not that module is
  in `srcs`. When it is, the `.mjs` is staged and leaves the type program:
  TypeScript keeps the higher-priority extension of a pair listed together, as
  `tsc` does. The checked-in file is the module's only declaration, and the
  rule declares no `.d.mts` output for it, since tsgo writes none.

  Gazelle classifies one as it classifies a `.d.ts`, with no directive: a
  script-mode one is ambient and joins every target in the directory, a module
  one joins the package target, and the target holding `compile.d.mts` answers
  for `./compile.mjs`. The test importing it gets the dep edge whether or not
  `# gazelle:ts_js_srcs` admits the JavaScript.
