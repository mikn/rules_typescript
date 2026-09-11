### Changed

- **A member's own name is a self-reference: the compile writes the manifest
  as built beside the `package.json` src, and a test inside the member depends
  on the compile alone.** Under the virtual store no importer links a member to
  itself, so a test importing its own package by name
  (`@lovable/canvas-sdk/wire` from `packages/canvas-sdk/src/exports.test.ts`)
  had no link to resolve through: Gazelle wrote no dep and one line per edge,
  and `TsgoDeclare` failed with `TS2307`. tsc, node and Vite resolve such a
  name through the nearest `package.json`'s `name` and `exports`, so
  `ts_compile` now writes the `package.json` at its package root as built --
  each source-file target rewritten to the emitted file by `tsaction manifest`,
  the rewrite `npm_store_member` did from the module extension's text -- as
  `<name>.package.json`, `TsInfo.manifest`, and stages the src as written like
  every other data file. The two readers that hold the emit take the copy as
  built: a dependent's program root lays the outputs of every first-party dep
  at or above the target's package over that package's directory and the dep's
  manifest as built at its `package.json`, so the manifest's `exports` reach
  the dep's declarations, and the member's store tree copies it as its
  `package.json`. A `ts_test`'s runfiles hold the src as written: a test that
  reads its manifest as data reads what the checkout has, and the package's
  own name lands on the source the `exports` name -- vitest transforms it, as
  the checkout's vitest does, and the node:test hook resolves the name from the
  test's own path, node's own first step, and maps the source to the compiled
  sibling. Gazelle resolves the self-reference as the first-party file it
  lands on: the member's `ts_compile` from a `ts_test` or a package below it,
  nothing from the member's own `ts_compile`, no line. `npm_store_member` loses
  `manifest_json`, `NpmStoreInfo` loses `manifest`, and `ts_test` no longer
  stages a member manifest of its own; a member whose compile lists no
  `package.json` in `srcs` fails the store naming the src to add.
