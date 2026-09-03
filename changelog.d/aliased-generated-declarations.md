### Fixed

- **A `path_aliases` alias now reaches the declaration a `css_library`,
  `asset_library` or `json_library` dep generated for a file under it.** The
  alias named only the source directory, where nothing sits beside the file.
  `rootDirs` bridges the source tree and `bazel-bin` for a relative specifier
  and TypeScript consults it nowhere else, so `import data from
  "#pkg/data.json"` was `TS2307` while `./data.json` type-checked. Each alias
  now maps onto its source directory and that directory's `bazel-bin` mirror,
  source first. Only a declaration the target already depends on is in the
  sandbox, so the dep still decides the answer. An aliased asset import that a
  wildcard ambient used to type now gets the same generated declaration a
  relative import does, `declaration_type` included.
