### Fixed

- **The generated tsconfig no longer carries what tsgo 7.0.2 reports and the
  `7.0.0-dev.20260311.1` nightly let pass.** A `types` entry a dep answers is
  dropped from the config: its declarations reach the compiler through `files`,
  and TypeScript resolves the entry itself through `typeRoots` and
  `node_modules`, neither of which the sandbox has, so `types = ["node"]` beside
  `@npm//:types_node` was `error TS2688: Cannot find type definition file for
  'node'`. The editor's tsconfig already dropped such entries; the build's now
  does the same. A path-shaped entry (`./typings`, `vendor/local.d.ts`) still
  reaches the compiler as written, and one no dep answers still fails at
  analysis.

  Under `declarations = "oxc"` the config now states `rootDir` too, as the exec
  root every input is under: a tsconfig in the chain that sets `outDir` made
  tsgo 7.0.2 check every source against a `rootDir` it inferred as the
  generated config's own directory, `TS6059` under `--noEmit`.
