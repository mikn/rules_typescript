### Fixed

- **A store file's `/// <reference types>` the chain answers is a dep Gazelle
  writes.** TypeScript's primary lookup for a type reference directive walks
  `node_modules/@types` up from the tsconfig's directory before the
  referencing file's own, so a store file's `/// <reference types="node" />`
  loads the `@types/node` an importer at or above the package declares. Gazelle
  writes that importer's label, `@npm//:types_node`, on the rule whose files
  reach the referencing file; a directive the chain does not answer resolved
  beside the referencing package's own tree and gets nothing. Without the link
  staged, the build's program fell to the secondary lookup and loaded the
  copies nested beside the referencing packages, two `@types/node` at once.
