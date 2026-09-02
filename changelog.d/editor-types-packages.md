### Fixed

- **The tsconfig an editor reads now resolves a `@types/x` package under the
  name it types, as the build already did.** `bazel run //:refresh_tsconfig`
  generates that file from its own pass over the build graph, and that pass
  skipped every `@types/*` package: their entry points were named in `files`,
  which brings the globals they declare into the program but answers no module
  specifier. So a file could type-check clean under `bazel build` and show
  `TS2307` in the editor, or the reverse, with nothing to say why -- the two
  configs are written by two code paths over one graph.

  The precedence is the same rule the build applies, from the same
  `types_package_alias` helper rather than a second copy: the runtime package
  answers `x` when it publishes declarations of its own, `@types/x` when it
  publishes none, `@types/a__b` demangles to `@a/b`, and a `path_aliases` prefix
  outranks both. A package reached only through its `@types/*` pairing --
  `@babel/core`, whose declarations are all in `@types/babel__core` -- had no
  entry in the editor map at all and now has one.

  A `@types/*` package is installed under `npm_dir` at its own name, which is
  where the `files` entry already put it, so the two routes share one copy. The
  key spelled `@types/x` stays out of the editor's `paths`: no import writes it.
  `//tests/npm_types_barename:test_config_agreement` now compares the npm half
  of both configs for one target and fails when they disagree.
