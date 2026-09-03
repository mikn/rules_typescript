### Added

- **`ts_test` can run node's own test runner: `runner = "node:test"`.** A test
  written against `node:test` registers with nothing vitest collects, so vitest
  reported `0 test` for the file and failed it as an empty suite; the runner
  was not selectable. `runner` defaults to `"vitest"`, so no existing target
  changes. A node:test target runs `node --test` over the same sharded file
  list, honours `--test_filter` as node's `--test-name-pattern`, and reports by
  exit status like the vitest path. It also installs an ESM resolver hook that
  retries a failed relative resolution with `.ts` rewritten to `.js`: oxc
  copies an `allowImportingTsExtensions` specifier into the `.js` it emits
  verbatim and only the `.js` is in the runfiles tree. The hook leaves user
  source and the emit untouched. The compile attributes (`lib`, `types`,
  `compiler_options`, `tsconfig`, `path_aliases`, `path_alias_srcs`,
  `types_srcs`) reach the node:test compile exactly as they reach the vitest
  one. Every vitest-shaped attribute (`config`, `environment`, `globals`,
  `reporters`, `setup_files`, `global_setup`, `snapshots`, the coverage trio,
  `update_snapshots`, `vitest`) is an analysis error under `"node:test"`, as is
  a dep providing `CssModuleInfo`, and `bazel coverage` on such a target fails
  instead of handing Bazel an empty report. No `<name>.update_snapshots` target
  is generated for it.
