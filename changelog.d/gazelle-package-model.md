### Breaking — gazelle

- **The `tsconfig.json` is the package, and `deps` come from tsgo's listing
  of it.** A directory is a package when its `tsconfig.json` lists a
  first-party file; a file belongs to the nearest package above it that lists
  it, and every other regular file under the package's tree is a data src.
  Gazelle writes `ts_compile(srcs, deps, tsconfig, visibility)`,
  `ts_test(srcs, deps, tsconfig, config)` and `ts_config(src, deps,
  visibility)` per package, and `Empty` for every kind under the names it
  would use in a directory that is not a package, naming the BUILD file to
  delete. Every dep
  is one edge of the listing mapped to a label: a `node_modules` file to its
  npm package through the lockfile, spelled `@npm//<importer>:<name>` when the
  nearest lockfile importer above the importing file declares the name and
  `@npm//:<name>` otherwise; a workspace member imported by name to its view
  `@npm//:<name>`, from every package but the member's own `ts_compile`; a file
  another package owns to that package's `ts_compile`; a file under a
  `ts_codegen`'s `out_dir` or among its `outs` to the codegen; a file no
  package owns to nothing, with one log line. A `ts_test`'s `deps` also carry
  its vitest config's imports and the nearest `package.json`'s `dependencies`
  and `devDependencies`, so no `# keep` holds `vitest` or a pool package.
  The listing resolves through the checkout's `node_modules`, so a root
  `pnpm-lock.yaml` with no install beside it stops the run. Gone with the
  every-directory generator: the package-boundary modes, the exclusion
  patterns, the data-file roll-up, the cycle report, the `_doc` target, the
  import lexer and every resolver table (relative, alias, `imports` map,
  ambient, npm, `/// <reference types>`, the JSX runtime, the Node built-ins),
  the Prisma, GraphQL and OpenAPI detectors with the `<name>_compile` they
  wrote, the npm inventory, and the `ts_pnpm`, `ts_add_package` and
  `node_modules` kinds. `KnownDirectives()` is empty: the thirteen remaining
  `ts_*` directives are gone, and core `# gazelle:exclude`, `# gazelle:resolve`
  and `# keep` are what a BUILD file says to Gazelle. The edit: a
  `tsconfig.json` in every directory that is to be a package, with the
  `include`, `files` and `exclude` that say what it holds; `pnpm install`
  before the run; delete every `ts_*` directive line and every BUILD file
  below a package that the run empties; write `ts_pnpm` and `ts_add_package`
  by hand at the root. The directives reference names what does each
  directive's job.
