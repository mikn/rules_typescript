### Fixed

- **A `config` from another package roots Vite at that package.** Vite's root
  was the test's package, so a relative path in a config Gazelle finds at the
  package root (`wrangler.configPath: "./wrangler.jsonc"`,
  `setupFiles: ["./test/setup.mjs"]`) resolved against the test's directory:
  `Could not read file: .../test/wrangler.jsonc`,
  `Cannot find module '.../test/test/setup.mjs'`. The root is now the config's
  package, the test's own with none. Vite's `cacheDir` is under `TEST_TMPDIR`.
