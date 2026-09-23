### Fixed

- **Gazelle now writes the `extends` chain into the `ts_config` it generates.**
  A `tsconfig.json` whose `extends` names another `tsconfig.json` by a relative
  path, one specifier or an array of them, gets `deps` on that directory's
  `ts_config` target, which is written there even where no program is. The
  parent file used to reach no action's inputs, so tsgo read the
  path out of the config, found nothing at it in the sandbox and reported
  `TS5083: Cannot read file` before it reached any question about the sources.
  Two shapes are still the
  author's, each named in the log: a base of another name (`tsconfig.base.json`)
  has no `ts_config` to name, and a package-form specifier resolves through
  node_modules, which the reader does not walk; a base outside the repository
  or at a path no file sits at gets no dep either. `deps` is Gazelle's to
  recompute on every
  run, so the run after the base moves or goes away corrects the label it
  wrote.
