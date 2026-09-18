### Changed

- **A `ts_test` checks the deps under its `tsconfig` from their sources.** A
  dep whose `tsconfig` is the test's -- the package's compile, which Gazelle
  names on both -- joins `TsgoCheck` as sources beside the test files, and its
  declarations reach the check by no path: the declarations a program reads
  are the `TsInfo.owners` records' of its closure, less the records of the
  deps held as sources, so a dep under another `tsconfig` between the test and
  the compile brings its own declarations and not the compile's. The test's
  check never waits for the compile's `TsgoDeclare`, and a type error in the
  package's sources fails it too; a dep under another `tsconfig` still arrives
  as declarations. The edges stay the compile's: an import from a test into a
  joined source is owned by the compile's label. `TsInfo.sources` holds the
  JavaScript srcs beside the TypeScript and declaration ones, and `TsInfo`
  gains `tsconfig`
  ([Providers](https://mikn.github.io/rules_typescript/rules/providers/#tsinfo)).
