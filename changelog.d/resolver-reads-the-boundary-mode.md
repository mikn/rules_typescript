### Fixed

- **A dep is no longer fabricated for a directory a package boundary rolled
  up.** Under `# gazelle:ts_package_boundary tsconfig` the target generator
  rolls a subdirectory holding no `tsconfig.json` of its own into the project
  above it and writes no BUILD file there. The resolver did not read that: a
  specifier landing in such a directory -- a `./preview.css` written in a
  rolled-up `scripts/preview.ts`, say -- still got a per-directory label, so
  Gazelle wrote a dep on a package it had itself declined to create. Bazel
  answers that with `no such package` **during analysis**, which fails every
  target in the build where the missing module alone would have been one
  `TS2307`. Measured on a monorepo trial under `//packages/...`: two such labels,
  `//packages/figma-plugin/scripts` and `//packages/workflow-sdk/src`, each
  named by the very target whose own `srcs` already listed the files. The guard
  added for a directory the generator refuses to walk read four hardcoded
  directory names -- dot-directories, `node_modules`, `dist`, `bazel-out` -- and
  never the boundary mode. Both halves are now one predicate, read by the
  roll-up walk, the framework staging walk, the `ts_config` target and the
  resolver alike. An indexed rule in such a directory still answers first, and a
  directory holding a `tsconfig.json` is a package whose label survives.
