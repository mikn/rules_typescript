### Fixed

- **A sharded vitest `ts_test` runs every file exactly once.** The launcher
  splits the compiled test files under both runners -- sorted by runfiles
  path, shard *i* of *n* running every *n*-th file from the *i*-th -- and
  stages a vitest shard's files alone for the walk; vitest's `--shard` is
  gone. That flag keyed a file by `resolve(root, moduleId).slice(root.length)`
  and sorted by the key's sha1, and the staged root's per-action name sat
  inside the cut, so the shards' orders differed: 16 shards of a 2,850-file
  suite ran 543 files twice and 543 never, every shard reporting green over
  the rest. A shard the split leaves no file exits 0 under either runner, so
  `shard_count` is no longer capped by the file count.
