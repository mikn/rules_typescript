### Fixed

- **The integration suite no longer downloads a copy of Bazel per test.**
  `bazel_binary` is not Bazel but a bazelisk wrapper, which defaults
  `BAZELISK_HOME` to `$PWD`; the harness runs it from the per-run workspace
  directory, so all 18 tests fetched Bazel from `releases.bazel.build` on every
  run -- roughly 1.2GB a suite, and a network dependency in each test that
  reddened a leg whenever a runner's DNS timed out. Green runs hid it, because
  Bazel echoes a test's stdout only when the test fails. The harness now points
  the nested bazelisk at a `bazelisk` directory beside the repository and disk
  caches it already shares, and CI restores that directory with them and primes
  it once before the suite -- bazelisk takes no cross-process lock, so a cold
  cache alone still had 7 of 9 tests fetching at the same instant. An inherited
  `BAZELISK_HOME` still wins, so a populated local cache is left alone.
