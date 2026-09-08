### Added

- **`ts_test` can run node's own test runner:
  `runner = "@rules_typescript//ts/runners:node_test"`.** A test written
  against `node:test` registers with nothing vitest collects, so vitest
  reported `0 test` for the file and failed it as an empty suite; the runner
  was not selectable. `runner` defaults to `//ts/runners:vitest`, so no
  existing target changes. A node:test target runs `node --test` over the same sharded file
  list, honours `--test_filter` as node's `--test-name-pattern`, and reports by
  exit status like the vitest path. The package's code runs at its runfiles
  paths, as under vitest: the entry keeps its path (`--preserve-symlinks-main`)
  and a `node:module` resolve hook resolves a relative specifier to the file
  the compiled tree holds for it -- the compiled sibling of a `.ts`, the `.js`
  or `index.js` of an extensionless one, the file as written otherwise -- and a
  bare specifier from the test's own node_modules tree. The hook leaves user
  source and the emit untouched. `tsconfig` reaches the node:test compile
  exactly as it reaches the vitest one. Every vitest attribute (`config`,
  `config_srcs`, `coverage_provider`, `wrangler_config`) is an analysis
  error under the node:test runner, and `bazel coverage` on such a target
  fails instead of handing Bazel an empty report.
