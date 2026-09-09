### Added

- **`ts_test` takes `args`, the runner's command-line flags.** Under
  `runner = "node:test"` they are node's, placed before `--test` so that the
  children `node --test` spawns inherit them: `mock.module` exists only under
  `--experimental-test-module-mocks`, which `NODE_OPTIONS` refuses, so a suite
  that mocks modules sets `args = ["--experimental-test-module-mocks"]` as its
  package's test script does. Under vitest they follow `vitest run --config`.
  `bazel test --test_arg` appends to them on either runner.
