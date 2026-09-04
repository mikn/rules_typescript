### Fixed

- **Gazelle removes a generated `asset_library`, `json_library`, `css_library`
  or `css_module` whose file is gone.** A plain `srcs` list on those rules is
  read back on later runs as a claim on the file, so deleting the file left the
  rule behind, naming a source Bazel could not find, until the BUILD file was
  regenerated from nothing. A rule of those kinds whose `srcs` name only files
  no longer on disk is now reported empty, and the merger deletes it the way it
  deletes a `ts_compile` with no sources. A `srcs` holding a label, a file a
  rule in the package generates, or a `glob()` is left alone; `# keep` above
  the rule holds it.
