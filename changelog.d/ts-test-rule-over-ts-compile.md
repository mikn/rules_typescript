### Breaking — ts_test

- **`ts_test` is a rule over `ts_compile`'s attributes, and `runner` names a
  runner target.** The test target compiles `srcs` itself with `ts_compile`'s
  actions, and the `node_modules` forest tsgo checked them against is the tree
  they run in, so the `_<name>_compile` and `_<name>_node_modules` targets the
  macro generated no longer exist and `node_modules` and `npm_workspace_name`
  are gone: an IDE tsconfig lists the test itself, and `ts_refresh_tsconfig`'s
  targets are testonly for it. `runner` is a label, `//ts/runners:vitest` by
  default; `runner = "node:test"` becomes
  `runner = "@rules_typescript//ts/runners:node_test"`. `<name>.reads` becomes
  `bazel run //path:my_test -- --reads`. The snapshot updater is gone,
  `<name>.update_snapshots` and `update_snapshots` with it; writing a `.snap`
  is vitest's own `vitest -u` in the package. A test's own files leave the
  coverage report: they are a test target's, which Bazel excludes unless
  `--instrument_test_targets` is set.
