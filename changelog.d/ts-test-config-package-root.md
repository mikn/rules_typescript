### Fixed

- **A `config` from another package roots Vite at that package, and a config
  installing `@cloudflare/vitest-pool-workers` runs on realpaths.** Vite's root
  was the test's package, so a relative path in a config Gazelle finds at the
  package root (`wrangler.configPath: "./wrangler.jsonc"`,
  `setupFiles: ["./test/setup.mjs"]`) resolved against the test's directory:
  `Could not read file: .../test/wrangler.jsonc`,
  `Cannot find module '.../test/test/setup.mjs'`. The root is now the config's
  package, the test's own with an inline dict or none. When the config's
  `plugins` hold the pool's, the Bazel layer sets `resolve.preserveSymlinks:
  false` in place of the config having to, and adds a plugin that resolves a
  compiled module's relative imports from its runfiles path, its bare imports
  from the root, and refuses an import resolving to a build output the runfiles
  do not hold at its own path. Vite's `cacheDir` is under `TEST_TMPDIR`.
