### Fixed

- Gazelle selects Istanbul coverage for Workers tests only when an ancestor
  importer declares the provider. Packages present only transitively or in a
  sibling importer no longer produce unresolved test dependencies.
