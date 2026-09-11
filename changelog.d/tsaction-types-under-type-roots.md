### Fixed

- **A tsconfig chain that sets `typeRoots` gets no `types` from the direct
  `@types/*` deps.** tsaction wrote the deps' names whenever the chain set no
  `types`, and TypeScript skips the `node_modules` walk for a `types` name under
  a custom `typeRoots`, so a program whose chain named a `node_modules/@types`
  pnpm never creates failed `TS2688: Cannot find type definition file for
  'node'` where its checkout compiled. The chain's `typeRoots` bounds automatic
  inclusion as it does in the checkout, and a store file's `/// <reference
  types>` resolves beside the referencing file. A chain with no `typeRoots` is
  written as before.
