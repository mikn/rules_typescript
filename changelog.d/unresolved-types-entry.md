### Breaking — ts_compile

- **A `types` entry in `compiler_options` that no dep answers is now an analysis
  error.** A `types` entry names a package, and TypeScript resolves it by
  walking `node_modules`, which this ruleset does not have, so the rule
  resolves the entry from `deps` and puts the declaration the package's manifest
  designates into the generated tsconfig's `files`. An entry no dep answered
  used to resolve to nothing, silently. `tsc` does report one: `error TS2688:
  Cannot find type definition file`, exit 2, with and without a `typeRoots`
  holding a real type package (typescript 5.9.2), and so does tsgo 7.0.2, from
  the action and naming no dep. The `7.0.0-dev.20260311.1` nightly printed
  nothing on either probe and exited 0; it did report `TS2688` for a
  `/// <reference types="…" />` in a source file, so the silence was this
  option's. Through the rule the same entry built green with no output at all,
  and the error landed later on whatever used the missing declarations:
  `TS2339` on `import.meta.env` for a missing `vite/client`, `TS2591` on
  `process` for a missing `node`, `TS2307` on `cloudflare:test` for a missing
  pool package. It is an error, not a warning: analysis output is not replayed
  on a cache hit, so a `print()` here appears on the build that analyses the
  target and on no build after it.

  The check covers `ts_test`, which folds `types` into a `ts_compile` of its
  own. Four spellings resolve: the package itself, one of its `exports`
  subpaths (`vite/client`), a subpath the manifest leaves unnamed that the
  package ships a declaration for (`@cloudflare/workers-types/2023-07-01`), and
  the bare name a paired `@types/*` package supplies (`node` is `@types/node`).
  An entry naming a path (one starting with `.` or `/`, or ending in `.d.ts`)
  is no dep's to answer, so this check leaves it alone; a `./` or `../` one is
  resolved from `srcs` and `types_srcs` instead, and guarded there. A blank
  entry is guarded by neither; whitespace is trimmed before any of that, which
  is the reading Gazelle's `ambientTypeLabel` gives an entry before it writes
  the dep.

  Add the dep that publishes the package (`@npm//:vite` for `vite/client`); the
  message names the entry and, for a package that is a dep already, the
  subpaths it does designate. A target whose entry resolves from a `typeRoots`
  directory is exempt: what sits under one is the compiler's to find at action
  time and the rule cannot see it. State that `typeRoots` in `compiler_options`
  if it is only in a `tsconfig` file the rule cannot read.

  Only this attribute is checked. A `types` in the `tsconfig` file a target
  names is a layer the rule does not read, so nothing resolves those entries and
  nothing guards them: a target whose `tsconfig` holds
  `"types": ["vite/client"]` and whose `deps` hold `@npm//:vite` analyses
  without complaint, generates a config whose `files` is empty, and fails in
  the compiler: `TS2688` on the entry from tsgo 7.0.2, `TS2339` on the
  `import.meta.env` the declarations never typed from the nightly. Put the
  entries in `compiler_options`.
