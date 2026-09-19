### Breaking — node_modules

- **The lockfile's root importer names its hidden hoist.** `node_modules`
  takes `hoist`, the lockfile's `npm_store_hoist` target
  `:node_modules/.pnpm/node_modules`, where every other importer takes
  `parent`: one of the two, and the `deps` come from the hoist's lockfile.
  `npm_store_hoist` returns `NpmHoistInfo`, and every importer on a chain
  carries it as `NodeModulesInfo.hoist`; a workspace member the root does not
  link is hoisted by name alone (`NpmHoistInfo.members`), with no dependency
  on the member's tree, so the root's `node_modules` depends on no member's
  compile. Gazelle writes the attribute on the lockfile package's
  `node_modules`. The edit: add
  `hoist = ":node_modules/.pnpm/node_modules"` to every hand-written
  `node_modules` with no `parent`, spelled from the lockfile's package when
  the target sits elsewhere (`//tests/npm:node_modules/.pnpm/node_modules`).
