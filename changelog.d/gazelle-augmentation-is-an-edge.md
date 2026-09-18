### Fixed

- **A `declare module "<name>"` augmentation is an edge.** tsgo resolves the
  augmented module as it resolves an import and adds no file, so
  `--explainFiles` listed the module's file with every import that reached it
  and never the augmentation; Gazelle wrote no dep, the importer's link was
  not staged, and `TsgoCheck` reported `TS2664` in the sandbox. The
  source-built compiler (`module-augmentation-include-reason.patch`) lists
  `Augmented via "<name>" from file '<augmenting file>'` for a module the
  program holds; the parser reads it as an edge of its own kind, Gazelle
  writes the package's label as it would for an import, and tsaction's
  strict-deps check reports it as `augments`.
