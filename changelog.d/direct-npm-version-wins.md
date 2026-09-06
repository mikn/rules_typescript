### Fixed

- **A target's node_modules forest links, at the top level, the version of an
  npm package the target declared.** The packages were collected in one pass
  over `deps`, each dep followed immediately by its transitive closure, and the
  first writer kept the name. A dependency listed earlier handed the compiler
  its own older copy of a package the target also depends on directly. In the
  Lovable monorepo `//web:web` listed `@npm//:firebase` well before
  `@npm//:web-vitals`, and `web-vitals` resolved to the 4.2.4 inside
  `@firebase/performance`, not the 6.0.1 pnpm installed for that importer. The
  4.2.4 declarations carry none of the fields the application reads. Direct
  deps now claim their names before any transitive one is offered; transitive
  deps still fill every name no direct dep claims
  (`//tests/npm:direct_version_wins_test`).
