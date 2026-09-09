### Added

- **`ts_test` takes `config_srcs`, the modules `config` imports.** A config's
  relative import of the package's own module resolved to nothing (`Could not
  resolve './plugins/foo'`, a Startup Error before any test ran): the run's
  tree held the config alone. `config_srcs` names the modules the config
  imports and the ones they import, each at its own path in the runfiles, so
  `./plugins/foo` resolves beside the config. Gazelle writes the attribute
  from the config's listing, followed through every first-party module it
  reaches; the closure's npm edges are deps. A config whose imports stay bare
  is unchanged.
