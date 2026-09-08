### Breaking — ts_test

- **`ts_test` has ten attributes, and every vitest setting is the `config`
  file's.** `ts_compile`'s `srcs`, `deps` and `tsconfig`; `runner` and `env`;
  the vitest runner's `config`, `config_srcs`, `data`, `coverage_provider`
  and `wrangler_config`. Deleted: `environment`, `globals`, `setup_files`,
  `global_setup`, `reporters`, `coverage_thresholds`, `snapshots`, `coverage`,
  `vitest`, `runtime`, and the dict form of `config`. The generated config is
  Bazel's layer under the file, `coverage_provider` and the snapshot redirect
  over both, so a `test.environment`, `test.globals`, `test.setupFiles`,
  `test.globalSetup`, `test.reporters` or `test.coverage.thresholds` the file
  sets is what runs, as under plain `vitest`; a `setupFiles` entry naming a
  source runs its compiled sibling, so the `ts_compile` over it is in `deps`.
  `ts_test` is the rule: the macro that compiled `setup_files` and
  `global_setup` is gone with the `_<name>_setup` and `_<name>_global_setup`
  targets. A `.snap` is a src of the test, as every other file under the
  package is; `bazel coverage` needs no opt-in, and `coverage.enabled` in the
  file is what instruments a plain `bazel test`. vitest is the one in the
  test's `node_modules` tree, which the runner requires in `deps`; the JS
  runtime is the toolchain's.

  Migration: move each setting into the package's vitest config and name the
  file in `config`; list the `.snap` files in `srcs`, or let Gazelle; drop
  `vitest`, `runtime` and `coverage`.
