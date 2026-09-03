### Fixed

- **Gazelle writes `path_aliases` on the `ts_test` it generates, and
  `path_alias_srcs` on any target whose aliased imports no src of its own
  validates.** A package whose sources import through a `paths` alias got a
  `ts_compile` carrying the alias and a `ts_test` carrying nothing, so every
  aliased import in a test file was `TS2307` until the attribute was written by
  hand: the test files are a program of their own, and the alias map on the
  package target reaches nothing they compile. `path_alias_srcs` is filled in
  at resolve time like `deps`, naming the target the aliased import resolved to,
  and only when none of the target's own srcs sits under the alias directory --
  a src under it validates the alias by itself, and the aliased declarations
  arrive on the dep edge, so naming the target there would stage every output
  it has for nothing. The same guard runs on a `ts_compile`, so a `ts_compile`
  importing across an alias boundary gets the attribute too, where it used to
  fail analysis on the first build after Gazelle.
