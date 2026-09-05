### Fixed

- **A `ts_test` importing its own workspace member by name depends on the hub
  label.** A test inside a pnpm workspace member that imports the member by its
  package name through the member's own `exports` map got the local module
  target the manifest designates, the dep a `ts_compile` inside the member gets
  because the hub target's `target` is that compiling target. The local target's
  `TsModuleInfo` carries no name, so the test's generated tsconfig had no `paths`
  key for the specifier and the compile failed with `TS2307: Cannot find module
  'shared/wire'`. From a `ts_test`, whose compile target is never the hub's, the
  member's name now resolves as any other bare specifier does, to
  `@npm//:<member>` through the lockfile gate, and the hub's `TsModuleInfo`
  writes the name and every declared subpath into `paths`. A `ts_compile` inside
  the member keeps the local target. The runtime link is unchanged: the hub's
  generated `package.json` names no entry and no `exports`, so such a test
  type-checks and, unless the member is entered through a root `index`, fails in
  the resolver at run time.
