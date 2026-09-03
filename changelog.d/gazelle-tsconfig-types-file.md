### Added

- **A declaration file the project `tsconfig.json` names in
  `compilerOptions.types` now reaches every target Gazelle generates under it
  that has a type program.** `"types": ["./worker-configuration.d.ts"]`, as
  wrangler writes it, used to reach none of them. The generated per-directory
  config states its own `files`, `include` and `exclude` and takes
  `compilerOptions` from the project file through `extends`, and TypeScript
  resolves a relative entry against the config the program was invoked with,
  which is the generated one in `bazel-out`. The entry named nothing, and every
  global that file declares was `TS2304` in every directory below. On the
  roundtrip fixture added here, with the entry inherited and the file staged
  on a dep edge: `TS2552` on `WorkerEnv`, `TS2304` on `WORKER_BUILD_ID`.

  Gazelle rebases the entry onto the four kinds that type-check (the package
  `ts_compile`, the framework client entry, the `_doc` compile and the
  `ts_test`) and names the file by a `filegroup` beside the tsconfig. The
  other kinds it writes there carry no type program and no entry:

  ```python
  # workers/proxy/BUILD.bazel
  filegroup(
      name = "tsconfig_types",
      srcs = ["worker-configuration.d.ts"],
      visibility = ["//visibility:public"],
  )

  # workers/proxy/src/BUILD.bazel
  ts_compile(
      name = "src",
      srcs = ["handler.ts"],
      tsconfig = "//workers/proxy:tsconfig",
      types = ["../worker-configuration.d.ts"],
      types_srcs = ["//workers/proxy:tsconfig_types"],
  )
  ```

  The whole `types` list is written, not the file entries alone: `types` is one
  key and `extends` replaces it whole, so a subset would drop the packages the
  project asked for.

  Nothing propagates. `types_srcs` stages a file into one program and travels on
  no dep edge, and no `public_globals` is written, so a consumer outside the
  tsconfig's subtree that depends on a target inside it does not get the
  declaration. It fails on the identifier, as before.

  Only `compilerOptions.types` drives this. A declaration named in `include`
  gets nothing: `include` does not survive `extends` into the generated config,
  so it states nothing about the tree below it. An entry naming a path outside
  the tsconfig's own directory, or a file that is neither there nor written by
  a `ts_worker_types` target in the tsconfig's BUILD file, is logged and
  produces nothing. For a file such a target writes, the label is that target,
  not the filegroup; see the `ts_worker_types` entry.

  Neither attribute is mergeable, so deleting the lines does not opt out:
  `rule.MergeRules` copies in an attribute the rule does not carry at all, and
  they come back on the next run. `types = []` with `types_srcs = []` sticks and
  asks for no ambient types at all; a `# keep` above the whole `ts_compile`
  sticks and leaves the entries where `extends` puts them.
