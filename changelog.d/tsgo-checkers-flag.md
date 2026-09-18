### Added

- **`--//ts:checkers=N` sizes tsgo's checker threads.** TsgoCheck and
  TsgoDeclare run with `--checkers N` and declare `cpu:N`, so Bazel schedules
  each as N cpus. `0`, the default, leaves tsgo's own count -- four -- and
  Bazel's estimate of one. Over a 26,496-file program `--checkers 16` checked
  in 10.8-11.2 s where the default took 13.0-13.6 s.

  ```bash
  bazel build //... --//ts:checkers=16
  ```
