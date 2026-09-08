### Changed

- **`ts_compile` type-checks against a node_modules forest.** The tsgo action
  stages one tree artifact, `<name>/node_modules`, laid out as the runtime tree
  is: the target's npm deps, their closures, the `@types/*` package paired with
  each of them, and every first-party dep's npm closure, one entry per
  resolution with the target's own deps flat. tsgo runs from a program root
  that mirrors the exec root with that tree at `node_modules`, so a bare
  specifier, an `exports` condition, a `@types/*` pairing and a `types` entry
  resolve as tsc resolves them over a pnpm install, and a declaration it emits
  names packages the way the package's own `exports` allow. The action tsconfig
  is written by `tsaction tsconfig` from `tsgo --showConfig` over the ruleset's
  baseline and the target's `tsconfig`: it names no npm package in `paths`,
  lists the direct `@types/*` deps in `types` when the chain sets none, and
  leaves `typeRoots` unset, because a custom one stops tsgo's node_modules walk
  and that walk is where `@cloudflare/vitest-pool-workers/types`, an
  `exports`-only subpath, resolves. oxc transforms with the `target`, `jsx` and
  `jsxImportSource` the same file yields; the baseline now carries
  `target: es2022` and `jsx: react-jsx`, the values the rule used to inject
  over the file.
- **A workspace member's view carries the member's package.json as built.**
  `npm_hub` writes one `npm_workspace_package` per workspace member -- every
  `link:` target and every importer whose package.json has a name, one view per
  member directory, at `@npm//:<name>` -- and the view's `package.json` is the
  member's own with every source-file target under `main`, `module`, `browser`,
  `exports` and `imports` rewritten to the emitted `.js` and every `types`
  target to the `.d.ts`, key order kept. The forest and the runtime tree link it
  at `node_modules/<name>` beside the member's `.js` and `.d.ts` at the paths
  the manifest names, so a bare import and an `exports` subpath resolve for
  tsgo and for node through one manifest; the stub manifest that named no
  entry, and the `paths` entries `TsModuleInfo` derived from the member's
  `exports`, are gone. A member whose directory holds no package.json with a
  name gets a comment in the hub and no view. The editor tsconfig writes no
  `paths` key for a member: the checkout's node_modules holds pnpm's link to
  it.
