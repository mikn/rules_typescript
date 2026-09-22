### Fixed

- **A `ts_test` with `shard_count` runs without
  `--noincompatible_check_sharding_support`.** Under both runners the launcher
  touches `TEST_SHARD_STATUS_FILE` before the run, the file Bazel reads after
  a shard to know its runner honoured `TEST_SHARD_INDEX` and
  `TEST_TOTAL_SHARDS`. Before, the shards ran and Bazel discarded them:
  `Sharding requested, but the test runner did not advertise support for it
  by touching TEST_SHARD_STATUS_FILE`.
