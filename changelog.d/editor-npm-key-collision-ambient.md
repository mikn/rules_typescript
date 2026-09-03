### Fixed

- **The editor config no longer stages one declaration file twice when two npm
  packages claim a single `paths` key.** `chai` ships no declarations of its
  own and `@types/chai` holds them, so a closure with only `@types/chai` gives
  the alias the `chai` key while a closure with `chai` reaches the same
  declarations through `chai`'s own pairing. The root config aggregates
  closures, so both entries arrive at one key, and only the winner's entry was
  reconciled. The loser kept a `files` entry under its own installed
  directory, a byte-identical second copy of the file the winning key already
  resolves to. Every top-level name in it was declared twice: 14 `TS2300`s on
  the ruleset's own `chai` fixture, hidden because the generated root config
  sets `skipLibCheck: true`. The losing entry now names the winner's copy. It
  is repointed, not dropped: a direct `@types/x` dep asks for globals that
  `tsc` loads whatever shadows `x` for module resolution. Only a winner with
  no `types` entry of its own is that copy. One that names its own entry may
  also ship a file where the loser's sat, and naming that would swap the
  globals for an unrelated file. Those, and any path that is not installed,
  are dropped.
