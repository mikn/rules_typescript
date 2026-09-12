### Changed

- **A `ts_test` checks the deps under its `tsconfig` from their sources.** A
  dep whose `tsconfig` is the test's -- the package's compile, which Gazelle
  names on both -- joins `TsgoCheck` as sources beside the test files, with
  the declarations its own program read, so the test's check never waits for
  the compile's `TsgoDeclare` and a type error in the package's sources fails
  it too; a dep under another `tsconfig` still arrives as declarations. The
  edges stay the compile's: an import from a test into a joined source is
  owned by the compile's label. `TsInfo.sources` holds the JavaScript srcs
  beside the TypeScript and declaration ones, and `TsInfo` gains `tsconfig`
  and `deps_declarations`
  ([Providers](https://mikn.github.io/rules_typescript/rules/providers/#tsinfo)).
