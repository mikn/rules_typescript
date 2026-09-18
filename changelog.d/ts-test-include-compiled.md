### Fixed

- **A `config` whose `test.include` names the sources runs its tests.** The
  compiled `.js` the launcher hands vitest matched no `**/*.test.{ts,tsx}`
  glob, and vitest stopped with `No test files found, exiting with code 1`.
  The generated config now sets `test.include` to vitest's default pattern
  over the compiled extensions and reads none of the config's, so the run is
  the compiled tests whatever the config's globs say.
