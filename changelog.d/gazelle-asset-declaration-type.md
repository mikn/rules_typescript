### Added

- **`# gazelle:ts_asset_declaration_type <ext> <type>` writes
  `asset_library.declaration_type`.** Gazelle writes one `asset_library` per
  asset file, so a project running an svgr-style transform had to hand-edit the
  attribute onto every one of them and onto every asset added after. The
  directive applies to a directory and below. It reaches the targets already in
  the BUILD file as well as the ones a run writes: an existing `asset_library`
  has its `srcs` claimed and the generator emits nothing for it.

  ```python
  # gazelle:ts_asset_declaration_type .svg import("react").FC<import("react").SVGProps<SVGSVGElement>>
  ```

  Only the first space separates the extension from the expression, so
  `{ default: string; toc: string[] }` needs no quoting. A subdirectory
  overrides the type by naming the extension again, and returns its subtree to
  the `string` default by naming the extension with nothing after it, which
  removes the entry. An extension no directive in scope names is not Gazelle's
  and is left alone. `# keep` on the entry, the attribute or the rule holds a
  value against a directive that does name it.
