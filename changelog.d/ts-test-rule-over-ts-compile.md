### Breaking — ts_test

- **`ts_test` is a rule over `ts_compile`'s attributes, with ten attributes,
  and `runner` names a runner target.** `srcs`, `deps` and `tsconfig` are
  `ts_compile`'s: the test target compiles its srcs with the same actions, and
  the `node_modules` forest tsgo checked them against is the tree the tests run
  in, so the `_<name>_compile` and `_<name>_node_modules` targets the macro
  generated no longer exist and `node_modules` and `npm_workspace_name` are
  gone; an IDE tsconfig lists the test itself, and `ts_refresh_tsconfig`'s
  targets are testonly for it. `runner` is a label, `//ts/runners:vitest` by
  default; `runner = "node:test"` becomes
  `runner = "@rules_typescript//ts/runners:node_test"`. `env` and the vitest
  runner's `config`, `config_srcs`, `data`, `coverage_provider` and
  `wrangler_config` are the rest. Every vitest setting is the `config` file's: the generated config is
  Bazel's layer under the file, `coverage_provider` and the snapshot redirect
  over both, so a `test.environment`, `test.globals`, `test.setupFiles`,
  `test.globalSetup`, `test.reporters` or `test.coverage.thresholds` the file
  sets is what runs, as under plain `vitest`; a `setupFiles` entry naming a
  source runs its compiled sibling, so the `ts_compile` over it is in `deps`.
  Deleted: `environment`, `globals`, `setup_files`, `global_setup`,
  `reporters`, `coverage_thresholds`, `snapshots`, `coverage`, `vitest`,
  `runtime`, the dict form of `config`, the macro with its `_<name>_setup` and
  `_<name>_global_setup` targets, `<name>.update_snapshots` with
  `update_snapshots`, and `<name>.reads`. A `.snap` is a src of the test, as
  every other file under the package is; writing one is `vitest -u` in the
  package. `<name>.reads` becomes `bazel run //path:my_test -- --reads`. vitest
  is the one in the test's `node_modules` tree, which the runner requires in
  `deps`; the JS runtime is the toolchain's; `bazel coverage` needs no opt-in,
  and a plain `bazel test` passes vitest no coverage flags. A test's own files
  leave the coverage report: they are a test target's, which Bazel excludes
  unless `--instrument_test_targets` is set.

  Migration: move each vitest setting into the package's vitest config and
  name the file in `config`; list the `.snap` files in `srcs`, or let Gazelle;
  drop `vitest`, `runtime`, `coverage`, `node_modules` and
  `npm_workspace_name`; spell `runner` as the label.
