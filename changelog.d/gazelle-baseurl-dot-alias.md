### Fixed

- **A tsconfig `baseUrl` of `"."` no longer puts `./` on every generated
  `path_aliases` value.** `"@/*": ["./src/*"]` under that `baseUrl` was written
  as `"@/": "./src/"`, and `ts_compile`'s alias guard reads that as a directory
  none of the target's inputs live under -- `points at "./src/", where none of
  this target's inputs live` -- for a target whose every src is under `src/`.
  The value is now `"src/"`, the same as without a `baseUrl`.
