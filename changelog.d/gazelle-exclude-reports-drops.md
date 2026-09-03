### Added

- **A `# gazelle:ts_exclude` pattern now says what it dropped.** Excluded
  sources were collected only to diagnose a hand-maintained framework entry,
  and the rollup walk did not collect them at all, so a pattern matching more
  than it meant to removed files from the program in silence. A run now reports
  one line per pattern per package, naming the directive, the count, and up to
  three of the paths:

  ```
  typescript: web: # gazelle:ts_exclude vite.config.ts leaves 1 TypeScript file
  out of the srcs generated here: web/vite.config.ts. It names no path, so it
  matches that basename at every depth below this directory;
  "./vite.config.ts" anchors it here.
  ```

  Directives are inherited, so the package a drop fires in is usually not the
  package holding the line to edit. The line names the declaring build file
  when the two differ, and spells the anchored form that names the package the
  drop fired in: `"./web/*.gen.ts"` for a `*.gen.ts` declared at the workspace
  root and dropping a file in `web`.

  The report covers the srcs of that run and no further. Exclusion happens at
  generation time and never sees the merge, and `rule.MergeList` keeps a list
  element carrying `# keep`, so a hand-kept `srcs` entry goes on compiling an
  excluded file. A pattern that matched nothing in a package says nothing
  there, so a root-level `*.generated.ts` is quiet everywhere it does not
  apply. A pattern naming a directory is not reported: where it acts it stops
  the rollup walk before reading what is inside.
