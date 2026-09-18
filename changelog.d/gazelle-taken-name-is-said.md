### Fixed

- **A generated rule whose name a hand-written rule of another kind holds is
  named in the log.** Gazelle's merger drops such a rule without a word -- a
  `ts_codegen` named after its directory took the place of the package's
  `ts_compile` -- and the run now says which rule to rename.
