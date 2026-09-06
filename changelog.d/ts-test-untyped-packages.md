### Added

- **`ts_test` takes `untyped_packages` and forwards it to every `ts_compile`
  it generates** -- the one over `srcs` and the ones over the TypeScript
  entries of `setup_files` and `global_setup` -- with the meaning it has on
  `ts_compile`: the named package gets no `paths` key and no `files` entry in
  that compile's tsconfig, stays in `deps`, and no JavaScript moves. A test's
  program carries the npm closure of every dep, so a global-script package one
  first-party dep reaches leaks into it exactly as into a library's, and the
  macro had no attribute to say so.
