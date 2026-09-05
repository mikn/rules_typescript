### Breaking — ts_compile

- **A relative `compilerOptions.types` entry that no label answers is now an
  analysis error.** `types = ["./worker-configuration.d.ts"]` names a path, and
  TypeScript resolves `./x` and `../x` against the config's own directory. The
  entry resolves against the sandbox, where only what the action stages exists.
  Nothing staged the file, so the entry resolved to nothing, and the
  `7.0.0-dev.20260311.1` nightly said nothing about it, as for a package no dep
  publishes; tsgo 7.0.2 reports `TS2688` on it, naming no label. On the fixture
  added with this change, the entry rebased into the generated tsconfig
  correctly, the file it names appeared in none of the action's inputs, and the
  target failed under the nightly with `TS2304: Cannot find name
  'STAGED_AMBIENT'`, the global that declaration file declares. The
  unresolved-`types` check that landed before this one covered the package
  shape only.

  Name the file with a label and the entry resolves: `srcs`; a dep whose `srcs`
  hold it (a `.d.ts` in `srcs` is a declaration output unchanged, so the dep
  edge stages the source file itself, which is how the ruleset's own
  `//tests/compiler_options/worker` resolves its entry); or `types_srcs`, new
  in this release, for the file that is neither. An entry no staged source file
  sits at fails with the path it looked for and the attribute to list it in.
  The `typeRoots` exemption does not cover this shape: a relative entry never
  goes through `typeRoots`.

  The entry is written into the generated config as the path to the file it
  resolved to, so a generated declaration (a `ts_codegen` output, a
  `.d.ts` a genrule wrote) is named the way a checked-in one is: the entry as
  the tsconfig spells it, and the label that stages the file in `types_srcs`,
  in `srcs`, or on a dep edge. On the fixtures that ship with this: in the
  build, the previous rule refused such a target at analysis and this one
  type-checks it; in the package's editor program, the entry written against
  the source tree gives `TS2304: Cannot find name 'Env'` and written through
  `bazel-bin`, where the file is, gives 0 diagnostics.

  Two shapes stay the compiler's own. `./typings`, a directory, is rebased onto
  the generated config as before and walked at action time; which declaration
  inside it a name picks is answered only by reading the directory.
  `vendor/x.d.ts` is passed through as written: TypeScript resolves a name that
  is not `./` or `../` through `typeRoots` and `node_modules/@types`, not as a
  path, and `--traceResolution` shows tsgo taking those two routes.
