### Fixed

- **A tsconfig's `files` list orders the roots.** The pass that reads
  `--showConfig` wrote `files: []`, which tsconfig inheritance let override the
  chain's own `files`, so a `files`-based program lost its root order to the
  sorted srcs and the first-declared ambient pattern with it. The pass now
  writes `files: []` only when no file in the chain names `include` or `files`.
