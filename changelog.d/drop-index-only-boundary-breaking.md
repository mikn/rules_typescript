### Breaking — gazelle

- **`# gazelle:ts_package_boundary index-only` is gone; two modes remain.** The
  mode made a directory a package only when it held an `index.ts`/`index.tsx`,
  and it existed to be the pre-0.2.0 default kept as an opt-in. Everything it
  was reached for, `tsconfig` does by the unit `tsc` itself compiles: the shape
  that dissolves a package-level cycle — a barrel re-exporting `./rules` while
  `./rules` imports `../utils` — is one target over both directories either way,
  and an index file is not what says where a project's edge is. A tree carrying
  the directive moves to `# gazelle:ts_package_boundary tsconfig`, with a
  `tsconfig.json` in each directory that is to be a package and a
  `# gazelle:ts_package_boundary true` in any that has to be one without holding
  the file.

  The directive no longer accepts an unrecognised value at all: it was a warning
  plus the inherited mode, and it now stops the run, naming `index-only`
  specifically as removed and naming the two modes that are left. The boundary
  mode decides which files each target compiles, so a directive that quietly did
  nothing left a tree compiling to something other than what its author wrote.
