### Breaking — gazelle

- **`# gazelle:ts_package_boundary index-only` is gone; two modes remain.** The
  mode made a directory a package only when it held an `index.ts`/`index.tsx`,
  and it existed as the opt-in that restored the behaviour from before 0.1.0.
  `tsconfig` covers the same case by the unit `tsc` itself compiles: the
  package-level cycle of a barrel re-exporting `./rules` while `./rules`
  imports `../utils` is one target over both directories in either mode. A
  tree carrying the directive moves to `# gazelle:ts_package_boundary tsconfig`,
  with a `tsconfig.json` in each directory that is to be a package and a
  `# gazelle:ts_package_boundary true` in any that has to be one without holding
  the file.

  The directive no longer accepts an unrecognised value. It was a warning plus
  the inherited mode; it now stops the run, naming `index-only` as removed and
  naming the two modes that are left. The boundary mode decides which files
  each target compiles.
