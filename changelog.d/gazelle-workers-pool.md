### Added

- **Gazelle writes a Workers-pool test's `wrangler_config` and
  `coverage_provider`.** A vitest config naming its wrangler config in a
  string literal (`wrangler.configPath`) puts `filegroup(name =
  "wrangler_config")` over the file in its package, beside `vitest_config`,
  withdrawn when the literal or the file goes. A `ts_test` whose config imports
  `@cloudflare/vitest-pool-workers` gets `wrangler_config` naming it -- the
  filegroup's label from a package below, the file's name from the config's
  own package -- and, when the lockfile declares `@vitest/coverage-istanbul`,
  `coverage_provider = "istanbul"` with the package in `deps` in the
  importer's spelling; without it the run says so and writes neither. Both
  attributes are Gazelle's, recomputed with `deps`; a hand-set value survives
  under `# keep`.
