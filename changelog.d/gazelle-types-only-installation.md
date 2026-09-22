### Fixed

- **A bare import that resolves into a `@types/<name>` package installed with
  no `<name>` beside it gets that package as its dep.** A bare specifier's
  package was the edge's name whatever the lockfile said, so `import type
  { Text } from "mdast"` under an importer declaring `@types/mdast` alone was
  refused as a name the lockfile never mentions and the build failed with
  `TS2307`. The specifier's package is the label when the lockfile mentions it;
  otherwise the package the listed file belongs to is, `@npm//web:types_mdast`.
