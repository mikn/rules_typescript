### Breaking — ts_compile

- **A relative `compilerOptions.types` entry that no label answers is now an
  analysis error.** `types = ["./worker-configuration.d.ts"]` names a path, and
  TypeScript resolves `./x` and `../x` against the config's own directory — so
  the entry resolves against the sandbox, where only what the action stages
  exists. Nothing staged the file, so the entry resolved to nothing and, as for
  a package no dep publishes, tsgo said nothing about it. On the fixture added
  with this change, the entry rebased into the generated tsconfig correctly, the
  file it names appeared in none of the action's inputs, and the target failed
  with `TS2304: Cannot find name 'STAGED_AMBIENT'` — the global that declaration
  file declares. The unresolved-`types` check that landed before this one
  covered the package shape only, so this shape stayed silent.

  Name the file with a label and the entry resolves: `srcs`, a dep whose `srcs`
  hold it (a `.d.ts` in `srcs` is a declaration output unchanged, so the dep
  edge stages the source file itself — which is how the ruleset's own
  `//tests/compiler_options/worker` resolves its entry), or
  `types_srcs`, new in this release, for the file that is neither. An entry no
  staged source file sits at fails with the path it looked for and the attribute
  to list it in. The `typeRoots` exemption does not extend to this shape: a
  relative entry never goes through `typeRoots`.

  A dep's *generated* declaration is not nameable this way, whatever its
  package-relative path looks like: the entry resolves against the source tree
  and that file is in `bazel-out`. The message says so, and points at
  `public_globals` — the route a generated ambient takes into a consumer's
  program.

  Two shapes stay the compiler's own. `./typings`, a directory, is rebased onto
  the generated config as before and then walked at action time — which
  declaration inside it the name picks is a question only reading the directory
  answers. `vendor/x.d.ts` is passed through as written, because TypeScript
  resolves a name that is not `./` or `../` through `typeRoots` and
  `node_modules/@types` rather than as a path; `--traceResolution` shows tsgo
  taking those two routes.
