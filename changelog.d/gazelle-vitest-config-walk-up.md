### Fixed

- **Gazelle finds a `ts_test`'s vitest config where `vitest` does.** The
  generator read `vitest.config.*` from the test's own directory only, so a
  package keeping its config at the root beside `package.json` with its tests
  under `test/` got a `ts_test` with no `config`, and the generated config's
  user layer was empty: a worker's `defineWorkersConfig` pool ran as no pool,
  and a `test.server.deps.inline` entry never applied, so an ESM dependency
  with extensionless relative imports failed at import time under Node. With
  no config beside the tests, Gazelle now walks up to the nearest directory
  holding a `package.json`, or the repository root, and writes the config it
  finds there as `config = "//<pkg>:vitest_config"`: a public `filegroup` over
  the file, which the owning package's BUILD file gains and which the next run
  matches by name. A bare specifier the config imports is a dep of the test as
  before; Gazelle reads a relative import in a config above the tests against
  the test's package, not the config's. A config beside the tests still wins, a
  `package.json` in the test's own directory ends the walk there, and a config
  in a directory with no `package.json` above the tests is passed over, as
  `vitest` run from the package root would pass over it.
