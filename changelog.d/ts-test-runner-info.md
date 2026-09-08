### Added

- **`TsTestRunnerInfo`: a test runner is a target.** `//ts/runners:vitest` and
  `//ts/runners:node_test` provide it, and a rule in another ruleset returning
  it is a third runner; `ts_test` compiles the tests and builds the forest, and
  the runner's `launch` writes the launcher config and names the runfiles. The
  provider is exported from `//ts:defs.bzl`.
