### Changed

- **The action's roots are the tsconfig's own `files`, `include` and
  `exclude`.** The written tsconfig carried a `files` entry per src and put
  the rest in `include`; `--explainFiles` finds a `files` root's reason by a
  scan of that list once per root, a minute over a 9,000-file program. Now
  each root spec of the chain is rewritten from the directory of the file
  that set it -- a chain naming neither `files` nor `include` gets tsc's
  default `**/*` over its directory -- so root order is tsc's own; `include`
  names each src no root names and each path-shaped `types` entry, one entry
  per path, under tsc's rules for a pattern's match.
  The program root the actions run from holds the action's source inputs at
  their paths and `bazel-out` whole, not the exec root's directories, so a
  pattern names the target's srcs sandboxed or not. `tsaction tsgo` and
  `tsaction emit` take the sources as `-source=FILE`.
