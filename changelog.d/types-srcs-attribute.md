### Added

- **`ts_compile(types_srcs = [...])`, the declarations a relative
  `compilerOptions.types` entry names.** A path in `types` resolves against the
  sandbox, so the file has to be staged by a label to be there; this is that
  label, for the declaration neither `srcs` nor a dep stages:

  ```python
  ts_compile(
      name = "lib",
      srcs = glob(["*.ts"]),
      types = ["../../worker-configuration.d.ts"],
      types_srcs = ["//workers/proxy:worker-configuration.d.ts"],
  )
  ```

  It is a label list, so the file may live in another package, and — unlike a
  `.d.ts` in `srcs` — it is not passed through as this target's own declaration.
  tsgo parses it as part of this program, so a syntax error in the file fails
  this target — measured: `TS1434`/`TS1005` from a broken staged declaration.
  What it declares goes unchecked, because it is a `.d.ts` under the baseline's
  `skipLibCheck`; `--//ts:lib_check` surfaces a type error inside it (`TS2552`
  on the same fixture) and the default does not. A file listed here that no
  `types` entry names is an analysis error: `types_srcs` is not `include`, so an
  entry is the only route it has into the program.

  Globals are what it is for. A module — a `.d.ts` with a top-level import or
  export — resolves and joins the program, but its declarations stay scoped to
  it, so nothing global arrives. `public_globals` rejects a module outright and
  this does not: a module in the program is what a module augmentation inside it
  needs.
