### Added

- **`tools/bench_forest.sh` times tsgo over a node_modules forest against the
  paths map.** It stages `//web:web`'s declare action as two execroot replicas,
  one with the action's tsconfig as Bazel writes it and one with a forest of
  symlinks in place of the npm `paths`, and records tsgo's check and declaration
  emit walls, the diagnostics of each, and the program's file set; the M7
  decision record reads its output.
