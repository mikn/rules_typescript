### Changed — ts_test

- **`tags = ["manual"]` on a `ts_test` now reaches every target the macro
  generates.** The tag went to the test rule alone, so `bazel build //...`
  skipped the test and went on to analyse the targets the macro derives from
  the same attributes and no BUILD file names: `_<name>_compile`,
  `_<name>_setup` and `_<name>_global_setup`, `_<name>_node_modules`, and
  `<name>.update_snapshots`. A manual test whose compile fails at analysis,
  which is how `//tests/compiler_options/analysis` asserts that a guard on
  `ts_compile` covers `ts_test`, stopped a wildcard build. `manual` is
  forwarded to all of them; every other tag still goes to the test rule alone,
  as a statement about how the test runs.
