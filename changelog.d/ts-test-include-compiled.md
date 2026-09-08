### Fixed

- **A `config` whose `test.include` names the sources runs its tests.** The
  compiled `.js` the launcher hands vitest matched no `**/*.test.{ts,tsx}`
  glob, and vitest stopped with `No test files found, exiting with code 1`.
  The generated config's Bazel layer now names the compiled test files in
  `test.include`, relative to the root, so the run is the rule's `srcs`
  whatever the config's globs say; a config without `include` is unchanged.
