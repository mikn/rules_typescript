### Fixed

- **`bazel coverage` on a `ts_test` writes its report.** The launcher wrote
  vitest's lcov to `COVERAGE_OUTPUT_FILE`, the file Bazel's `lcov_merger` then
  writes from the `.dat` files under `COVERAGE_DIR`, of which there were none,
  so under Bazel 9.2.0 every per-test `coverage.dat` and the combined report
  were empty while the run passed. The launcher now writes `vitest.dat` under
  `COVERAGE_DIR`, and `ts_test`'s merger is the rule's own,
  `@rules_typescript//tools/lcov_merger`: Bazel's keeps a record only under the
  coverage manifest's exact spelling, and the manifest names the `.ts` a target
  declared where the report names the `.js` compiled from it, so the rule's
  matches the two by path and keeps what `--instrumentation_filter` selected.
  The launcher resolves the report's paths against vitest's root, the config's
  package, where it had resolved them against the runfiles directory, so
  `../math.js` read `tests/vitest/coverage/math.js` and `same_package.js`
  stayed bare. `tools/ci/check_coverage_report.sh` runs the fixture under
  `bazel coverage` in CI and reads the report's `SF:` lines.
