### Breaking — ts_compile

- **The `TsStrictDeps` source scanner is deleted, with the `strict_deps` output
  group.** The check is the tsgo action's, over the edges tsgo itself resolved
  (under Changed). `--output_groups=strict_deps` names no group: a plain
  `bazel build` runs the check as part of `TsgoDeclare`, or of `TsgoCheck` in
  `_validation` under `--//ts:declarations=oxc`, where a finding fails the
  build without stopping the compile actions that waited for the stamp. A
  target with no program -- declarations alone in `srcs` -- runs no tsgo action
  and is not checked. A `ts_compile` with `deps` no longer needs a JS tool
  toolchain for the check.
