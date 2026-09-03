### Added

- **`ts_test` now takes `path_aliases` and `path_alias_srcs`,** forwarded to the
  `ts_compile` it generates for the test sources. `ts_compile` accepts both, but
  the `ts_test` macro's signature took neither, so a package whose sources import
  through an alias had a green `ts_compile` and a test target that could not be
  told about the alias at all — every aliased import in a test file failed with
  `TS2307`, and there was no attribute to set. The alias reaches type-checking
  only: oxc leaves the specifier alone, so a *value* import through an alias
  still fails in vitest with `Cannot find package`, and only a type-only import
  is erased. See
  [ts_test](https://mikn.github.io/rules_typescript/rules/ts-test/#path-aliases).
