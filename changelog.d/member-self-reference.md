### Changed

- **A member's own name is a self-reference: its compile stages the manifest
  as built, and a test inside the member depends on the compile alone.** Under
  the virtual store no importer links a member to itself, so a test importing
  its own package by name (`@lovable/canvas-sdk/wire` from
  `packages/canvas-sdk/src/exports.test.ts`) had no link to resolve through:
  Gazelle wrote no dep and one line per edge, and `TsgoDeclare` failed with
  `TS2307`. tsc, node and Vite resolve such a name through the nearest
  `package.json`'s `name` and `exports`, so `ts_compile` now stages every
  `package.json` in its `srcs` as built -- each source-file target rewritten to
  the emitted file by `tsaction manifest`, the rewrite `npm_store_member` did
  from the module extension's text -- and the store tree, a `ts_test`'s
  runfiles and a dependent's program root hold that one file at the member's
  path. The program root lays the outputs of every first-party dep at or above
  the target's package over that package's directory, so the manifest's
  `exports` reach the dep's declarations, and the ownership check reads a
  listed path through the root's link to the dep's output. Gazelle resolves
  the self-reference as the first-party file it lands on: the member's
  `ts_compile` from a `ts_test` or a package below it, nothing from the
  member's own `ts_compile`, no line. `npm_store_member` loses
  `manifest_json`, `NpmStoreInfo` loses `manifest`, and `ts_test` no longer
  stages a member manifest of its own; a member whose compile lists no
  `package.json` in `srcs` fails the store naming the src to add.
