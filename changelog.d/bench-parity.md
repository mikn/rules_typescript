### Added

- **`tools/bench_parity.sh` runs the parity benchmark protocol on a consumer
  checkout.** Four cache states (cold, warm, warm after a one-line edit,
  remote-cache-shaped), each cell three runs, the checkout's own commands
  (`.github/scripts/typecheck.sh`, the CI test rows, `tsc -p`, `vitest run
  --changed`) beside `bazel build //...` and `bazel test //...`; an `EXCLUDE`
  file leaves a red target and its checkout row out of the test-everything
  cells; one headed log per run and `summary.md` with medians and spread.
  A cell starts once other processes' CPU is under `OTHER_CORES` (2) over
  3 s, and a cell during which it averaged more is not a number: its log
  moves to `contended/` and its segment is redone, `REDO` (3) attempts
  before the runner stops. Kernel threads are not other processes, nor the
  `SYSTEM_PROCS` (a security sensor whose CPU follows the benchmark's own
  activity); the footer states each share. The result page is
  [Benchmark](https://mikn.github.io/rules_typescript/guides/benchmark/):
  the Lovable monorepo at parity, the mechanism behind each difference.
