### Fixed

- **The determinism check reads the files of the configuration it built.**
  The `determinism` job compared `$(bazel info bazel-bin)/<path>` between two
  output bases, and `pure = "on"` on the four `go_binary` targets is a
  rules_go transition that writes their binaries to a `-ST-<hash>` directory
  beside `bazel-bin`: the job's `ls` found no file and the compare never ran.
  The job is `tools/ci/check_determinism.sh DIR` now: `//tests/smoke:hello`
  and the four tools built from the empty output bases `DIR/a` and `DIR/b`
  with no disk or remote cache, and every file `cquery --output=files
  "config(..., target)"` names under the build's flags compared byte for
  byte.
