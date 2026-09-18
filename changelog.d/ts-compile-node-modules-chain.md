### Breaking — ts_compile

- **`ts_compile` and `ts_test` resolve npm packages through the importer chain
  `node_modules` names; the per-target forest is gone.** A new attribute,
  `node_modules`, names the `node_modules` target of the nearest lockfile
  importer at or above the package, and a direct npm dep resolves along it and
  its `parent`s nearest first, pnpm's walk-up: the link whose store is the
  dep's resolution is the one the program reads, a `@types/<name>` twin an
  importer links comes with it, and a workspace member is named by the
  importer's link target, `//<importer>:node_modules/<name>`, where the hub's
  view `@npm//:<name>` was named. A name no importer on the chain links fails
  analysis naming the nearest importer's `package.json`; a name an importer
  links at another resolution fails naming the label to write,
  `@npm//web:marked`; the view in `deps` fails naming the link target; a
  target whose closure holds an npm package and names no `node_modules` fails
  naming the attribute. The action stages the chain's links and the store
  trees and edge links their closures reach, `TsInfo.npm_files`, and nothing
  per target is built: `NodeModulesTree`, `<name>/node_modules`, the C/L/S
  manifest, the JS builder and the bash fallback are gone with
  `ts/private/actions/forest.bzl`. tsaction lays each importer's
  `node_modules` at the importer's directory in the program root, the
  lockfile's root importer's at the root's. A `ts_test` runs in the same
  layout: the runfiles hold the links and store files at their own paths, the
  launcher puts the chain's directories on `NODE_PATH` nearest first, links
  the chain's root in as `<workspace>/node_modules` where the walk up from
  the tests meets none, and in a manifest-only runfiles layout stages every
  `node_modules` entry of the manifest into the test root -- a declared link
  with its relative target verbatim, a store tree as a link to it. The
  node:test hook resolves a bare specifier from each importer's directory in
  turn and treats a file whose path holds a `node_modules/` segment as the
  store's, and `WranglerTestConfig` requires wrangler from the pool's realpath
  in the store, the pool's own edge. `NpmPackageInfo.direct_deps` is gone (no
  reader), `NodeModulesInfo` carries `label`, `dir`, `links` (name to
  `NpmLinkInfo(link, store)`) and `parent`, and an edge the extension cut to
  break a cycle is a link beside the store tree with no dependency behind it,
  as pnpm's store has it. Gazelle writes `node_modules` on every `ts_compile`
  and `ts_test`. The edit: add `node_modules = "//<importer>:node_modules"` to
  every hand-written target whose closure holds an npm package, and name a
  member's link target where the view was named.
