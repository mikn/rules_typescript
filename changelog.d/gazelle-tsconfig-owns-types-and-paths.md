### Breaking — gazelle

- **Gazelle writes no `types`, `types_srcs`, `path_aliases`, `path_alias_srcs`
  or `declarations`, writes no `tsconfig_types` filegroup, and the
  `ts_path_alias` and `ts_declarations` directives are gone.** The rule reads
  `paths` and `types` from the target's tsconfig and the emitter is
  `--//ts:declarations`, so a BUILD file has nothing to repeat. A path-shaped
  `types` entry puts the target that stages the file in `deps`: the
  `ts_codegen` at or above the tsconfig that declares it in `outs`, else the
  target whose `srcs` hold the checked-in declaration; a `paths` alias stays in
  the tsconfig, where the resolver reads it for `deps` (the first target of a
  fallback array, the one tsc tries first). The edit:
  `buildozer 'remove types' 'remove types_srcs' 'remove path_aliases' 'remove
  path_alias_srcs' 'remove declarations'` over the generated `ts_compile` and
  `ts_test` rules, `buildozer 'delete' //...:tsconfig_types`, and drop the two
  directives from your BUILD files.
