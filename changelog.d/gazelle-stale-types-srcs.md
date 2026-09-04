### Fixed

- **Gazelle takes back a `types_srcs` label it wrote once nothing stages the
  declaration by it, and withdraws a `ts_compile` whose every source is gone.**
  Moving a worker's checked-in `worker-configuration.d.ts` into a `ts_codegen`'s
  `outs` withdrew the `tsconfig_types` filegroup and left every rule below
  naming it: neither `types` nor `types_srcs` merges, so the value on disk won,
  and `no such target` failed analysis for each of those packages. On a
  `ts_compile` or `ts_test` by the kind and name the run generates, a
  `tsconfig_types` entry of the rule's own package or one above it whose
  filegroup the run does not write is now replaced with the labels the run
  stages the rule's entries by, the `ts_codegen`, or dropped when there are
  none, and the run logs each rule it rewrote; an entry naming a filegroup the
  run writes or a `# keep` holds stays, as does one naming anything else,
  another package's `tsconfig_types` included, and `# keep` on the entry or
  above the attribute holds even Gazelle's. In every-dir mode a directory with
  no source is no boundary, so the `ts_compile` whose only src was the deleted
  declaration was neither regenerated nor withdrawn; a package target whose
  plain `srcs` name only files that are gone, neither on disk nor the output of
  a rule in the package save a declaration a `ts_codegen` there writes, is now
  reported empty, with the `ts_lint` beside it, whether or not the directory
  still holds a `tsconfig.json`. A src that is a label or a `glob()` is not
  judged; `# keep` above the `ts_compile` holds it and the `ts_lint` with it.
