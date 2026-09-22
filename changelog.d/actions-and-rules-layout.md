### Changed

- **`ts_compile`'s and `ts_test`'s implementations follow rules_go's layout.**
  `ts/private/rules/` holds the two rule declarations (`ts_compile.bzl` exports
  `TS_COMPILE_ATTRS`) and `ts/private/actions/` one action per file: the
  TsConfig, OxcCompile, tsgo and TsStrictDeps actions, the
  generated vitest config, and the Workers pool's half of `ts_test`'s
  environment in `workers_pool.bzl`, the one file that names wrangler. Nothing
  loaded from `//ts:defs.bzl` changes, and no action changes.
