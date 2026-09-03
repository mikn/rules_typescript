### Fixed

- **A target's `paths` entry for an npm package names the version that target
  declared.** `pkg_info_map` was filled in one pass over `deps`, each dep
  followed immediately by its transitive closure, and the first writer kept the
  name. A dependency listed earlier handed the compiler its own older copy of a
  package the target also depends on directly. In the Lovable monorepo
  `//web:web` lists `@npm//:firebase` 153 entries before `@npm//:web-vitals`,
  and its generated tsconfig resolved `web-vitals` to the 4.2.4 inside
  `@firebase/performance`, not the 6.0.1 pnpm installed for that importer. The
  4.2.4 declarations carry none of the fields the application reads. Direct
  deps now claim their names before any transitive one is offered; transitive
  deps still fill every name no direct dep claims.
