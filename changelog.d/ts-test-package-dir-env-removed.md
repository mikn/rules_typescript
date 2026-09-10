### Breaking — ts_test

- **`TS_TEST_PACKAGE_DIR` is gone.** The variable anchored a path a config had
  to write absolutely while the config was loaded from a copy under
  `bazel-out`. The config is loaded from its package now: replace
  `process.env.TS_TEST_PACKAGE_DIR` with `__dirname` or `import.meta.dirname`,
  what the same config uses under plain `vitest`.
