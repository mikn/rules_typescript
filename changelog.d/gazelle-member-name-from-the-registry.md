### Fixed

- **A workspace member's name an importer takes from the registry gets that
  importer's package as its dep.** Every bare specifier naming a member went to
  the member view, which only an importer linking the member can spell, so
  `"example-transform": "1.1.13"` in `packages/bundler-plugin` -- a version of a name
  that is also the member `packages/transform` -- got no dep and one line,
  and the build failed with `TS2307`. The nearest importer at or above the
  package that has the name decides: its link target for a `link:`, the
  importer-scoped label, `@npm//packages/bundler-plugin:example-transform`, for a
  version; the line stays for a name no importer above links or declares.
