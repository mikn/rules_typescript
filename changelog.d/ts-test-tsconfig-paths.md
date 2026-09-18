### Fixed

- **A value import through a tsconfig `paths` alias resolves under `ts_test`.**
  oxc keeps the specifier in the compiled test, and vitest resolved it as a
  package: `Cannot find package '@app/x'`, or `Failed to resolve import` under
  a config with `resolve.tsconfigPaths`, no tsconfig being in the runfiles. The
  Bazel layer of the generated config now carries the chain's `paths`, read by
  tsaction as the compile reads them, and resolves an alias as tsc did -- the
  exact key, then the longest prefix; the values in order -- to the module in
  the runfiles, the compiled sibling for a `.ts` value. A type-only import is
  unchanged; under node:test an alias stays type-checking only.
