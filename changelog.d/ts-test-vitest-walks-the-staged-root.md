### Changed

- **vitest walks a root holding a `ts_test`'s compiled files alone.** The
  generated config's one include pattern was globbed over the runfiles tree:
  under `web/` 48,515 entries, 46,539 of them symlinks each stat'ed to be
  followed, 6.3 s against the checkout's 0.11 s over 32,704 regular files. The
  launcher now links the compiled files of the test's program -- this shard's
  under `shard_count` -- each at its runfiles path, into a directory of its
  own under `TEST_TMPDIR`, names it in `TS_TEST_FILES_ROOT`, and the generated
  config sets `test.dir` to it beside `test.include`, after the merge, on the
  root and on every project: the walk is the size of the run. A collected
  file's id is its runfiles path as before, so imports, snapshots and coverage
  are unchanged; a reporter names a file relative to vite's root, so by its
  staged path. A `ts_compile` dep's test-named file outside the program no
  longer runs.
