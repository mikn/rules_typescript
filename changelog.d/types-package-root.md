### Fixed

- **A package typed by a `@types/*` dep now resolves against that package, not
  against one of its declaration files.** The pairing recorded
  `dirname` of whichever paired declaration was listed first, so a `@types`
  package that keeps declarations in subdirectories was entered at a
  subdirectory: `@types/culori` has `all/`, `css/` and `fn/` beside its index,
  and `culori` resolved inside `all/` — `import { Color } from "culori"` was
  `TS2305` and `culori/fn` was `TS2307`. `NpmPackageInfo` now carries the
  paired package's own root, which is what npm resolves `x` from
  `node_modules/@types/x` as.
