### Fixed

- **Gazelle now writes the `extends` chain into the `ts_config` it generates,
  for the one specifier shape it can read without guessing.** A nested
  `tsconfig.json` whose `extends` is a single relative path to an ancestor
  directory's own `tsconfig.json` gets `deps` on that directory's `ts_config`
  target. The parent file used to reach no action's inputs, so tsgo read the
  path out of the config, found nothing at it in the sandbox and reported
  `TS5083: Cannot read file` before it reached any question about the sources.
  Three of the four own-failing targets under one worker in the monorepo trial
  died there, and a hand-written `deps` cleared it. Every other shape is still
  the author's: an `extends` array states a merge order and not which entry to
  stage, a package-form specifier resolves through node_modules, an absolute
  path resolves on one machine, and a base outside the repository, one no file
  sits at, or one in a directory Gazelle writes no `ts_config` into has no
  label to name. Each gets no `deps`. `deps` is Gazelle's to recompute on every
  run, so the run after the base moves or goes away corrects the label it
  wrote.
