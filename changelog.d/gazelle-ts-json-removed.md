### Breaking — gazelle

- **`gazelle_ts.json` is gone.** Nothing reads the file any more; a
  `gazelle_ts.json` left in a workspace is now an ordinary JSON file and lands
  in a generated `json_library`, so delete it once its keys have moved. Each key
  becomes a directive in a `BUILD.bazel` file, the root's or any ancestor of the
  directories it should govern:

  | Key | Write instead |
  |---|---|
  | `"pathAliases": {"@/": "src/"}` | `# gazelle:ts_path_alias @/ src/`, one per entry |
  | `"excludePatterns": ["*.gen.ts"]` | `# gazelle:ts_exclude *.gen.ts`, one per entry |
  | `"excludeDirs": ["coverage"]` | `# gazelle:ts_exclude_dir coverage`, one per entry (new) |
  | `"npmMappingFile": "npm/map.json"` | `# gazelle:ts_npm_mapping npm/map.json` (new) |
  | `"runtimeDeps": {"test": ["@npm//:happy-dom"]}` | `# gazelle:ts_runtime_dep @npm//:happy-dom`, one per label |

  A directive placed in the directory the file sat in reads the same as the key
  it replaces, with one difference: directives inherit and merge, where a nested
  `gazelle_ts.json` replaced the whole list an ancestor had built
  (`excludePatterns`, `excludeDirs` and `runtimeDeps.test` alike). Gazelle's own
  walk merges, so the two places that ask "which excludes apply here", the
  per-directory generation and the framework bundle's staging walk, answered
  differently depending on which directory asked. One run produced an invalid
  workspace and, elsewhere, a silently deleted target. If a subtree has to
  narrow what an ancestor declared, move the ancestor's directive down to the
  directories it is meant for.
