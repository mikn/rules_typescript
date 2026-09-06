### Added

- **`ts_test` takes `wrangler_config`, the file a Workers-pool `config` names
  through `wrangler.configPath`.** The pool boots the file `main` names, the
  source in a repository, which the runfiles do not hold: `Cannot find module
  '.../src/index.ts' imported from  cloudflare:test-...`. A build action patches
  a copy with wrangler's `experimental_patchConfig`, pointing `main` and every
  `env.<name>.main` at the compiled entry, and stages it at the source's
  runfiles path, so the config reads it. The file in `data` as well is an
  analysis error, and an `asset_library` copy of it among the deps is kept out
  of the runfiles. Every dep's `AssetInfo` files join the runfiles, which is
  what a wrangler `rules` module the worker imports needs.
