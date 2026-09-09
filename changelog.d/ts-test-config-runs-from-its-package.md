### Fixed

- **A `ts_test` runs vitest from the config's package, and `__dirname` in the
  config is that directory.** The launcher ran vitest from the runfiles root,
  so `join(process.cwd(), "shared/i18n/messages")` in a test opened
  `<runfiles>/shared/...` (`ENOENT`), and loaded the config from a copy beside
  the `node_modules` tree under `bazel-out`, so a plugin's `include` regex over
  `__dirname` matched no id vitest served (svgr: `InvalidCharacterError` on
  every `.svg` import). The run's directory is now the config's package in the
  runfiles -- the test's own with none -- where `pnpm run test` runs, and the
  launcher writes the config and its `config_srcs` at their own runfiles paths,
  and the generated config in the test's package, as regular files before the
  run: a runfiles entry is a symlink, and Vite bundles a config from its
  realpath, so through the symlink `__dirname` and the walk up to
  `node_modules` would start in `bazel-out`.
  `__dirname`, `import.meta.dirname` and `__filename` in the config and in a
  `config_srcs` module are the package's paths, as under plain `vitest`. A
  `config_srcs` entry outside the config's package is no longer an analysis
  error: it is written at its own path.
