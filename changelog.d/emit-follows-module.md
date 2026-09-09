### Fixed

- **A CommonJS-shaped program's JavaScript is CommonJS.** oxc's transform
  keeps the module syntax it reads, so a program whose tsconfig said
  `module: "commonjs"` -- or `nodenext` in a package with no `type` -- got ES
  modules, and node ran its node:test suites as such: `__dirname is not
  defined`, `Named export 'x' not found` from a CommonJS dependency. The emit
  action, `TsEmit` (was `OxcCompile`), reads the chain's `module` from the
  options file and hands such a program to `tsgo --noCheck`, run from the
  program root the check runs in; oxc emits the ES kinds and `preserve` as
  before. Under the node:test runner a `require` of a bare specifier is node's
  own resolution through `NODE_PATH`; the hook's tree lookup serves `import`.
  `//tests/node_test/cjs` pins the format at run time. The action tsconfig no
  longer sets `declarationDir`, which equalled `outDir`.
