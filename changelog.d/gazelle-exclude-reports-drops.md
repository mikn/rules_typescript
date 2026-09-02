### Added

- **A `# gazelle:ts_exclude` pattern now says what it dropped.** Excluded
  sources were collected only to diagnose a hand-maintained framework entry,
  and the rollup walk did not collect them at all, so a pattern matching more
  than it meant to removed files from the program in silence — the failure the
  exclusion mechanism exists to control. A run now reports one line per pattern
  per package, naming the directive, the count, and up to three of the paths:

  ```
  typescript: web: # gazelle:ts_exclude vite.config.ts keeps 2 TypeScript
  sources out of every generated target's srcs -- sub/vite.config.ts,
  vite.config.ts -- so nothing in the build compiles them. The pattern names no
  path, so it drops that name at every depth of this tree; "./vite.config.ts"
  anchors it to this directory.
  ```

  A pattern that matched nothing in a package says nothing there, so a
  root-level `*.generated.ts` is quiet everywhere it does not apply. A pattern
  naming a directory is not reported: it stops the walk before reading what is
  inside.
