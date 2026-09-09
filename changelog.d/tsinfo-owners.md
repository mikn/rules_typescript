### Added

- **`TsInfo.owners`.** A depset of `struct(label, files)`, one record per
  first-party target in the closure, this one first: the label a `deps` list
  writes for it and the declarations and data it stages. `ts_compile` and
  `ts_codegen` write their record; an npm package target and `ts_binary` leave
  it empty. The tsgo action reads the closure's records to name the target a
  listed file belongs to.
