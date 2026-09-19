### Fixed

- **A nested editor tsconfig now `extends` a package baseline in a parent
  package by a path that resolves.** The second `extends` entry was written as
  `./` plus the baseline's workspace-relative path, which is right only when
  the baseline sits in the nested package itself; a target in `workers/api/src`
  naming `//workers/api:tsconfig` got
  `./workers/api/tsconfig.json` from inside `workers/api/src` and the editor
  program failed `TS5083: Cannot read file`. A baseline outside the package is
  now written up through the workspace root, `../../../workers/api/tsconfig.json`.
