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
  `target: es2022`, `jsx: react-jsx` and `allowArbitraryExtensions`, the values
  the rule used to inject over the file.
