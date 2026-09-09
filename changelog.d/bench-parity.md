### Added

- **`tools/bench_parity.sh` runs the parity benchmark protocol on a consumer
  checkout.** Four cache states (cold, warm, warm after a one-line edit,
  remote-cache-shaped), each cell three runs, the checkout's own commands
  (`.github/scripts/typecheck.sh`, the CI test rows, `tsc -p`, `vitest run
  --changed`) beside `bazel build //...` and `bazel test //...`; one headed
  log per run and `summary.md` with medians and spread. The result page reads
  its output.
