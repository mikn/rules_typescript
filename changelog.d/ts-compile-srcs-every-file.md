### Added

- **`ts_compile.srcs` accepts every file.** A src that is neither TypeScript,
  JavaScript nor a declaration is a data file: staged into the output tree
  unchanged at its package-relative path, so the compiled module beside it
  reaches it by the relative path the source used, and carried to consumers as
  `TsInfo.data` and `TsInfo.transitive_data`, which `ts_test` stages
  in the runfiles beside the `.js`, `ts_binary` in its runfiles and its bundle
  and `ts_dev_server` in its runfiles. A `.json` src is also a tsgo input:
  an import of it resolves to the file and is typed from its contents under
  `resolveJsonModule`, which bundler resolution implies, and tsc reads the
  nearest `package.json` of every source for the module's format and the
  package's own name. A `.mts` or `.cts` src is refused: the rule emits `.js`
  and `.d.ts` from `.ts` alone.
