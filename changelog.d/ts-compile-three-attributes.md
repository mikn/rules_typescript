### Breaking — ts_compile

- **`ts_compile` has three attributes: `srcs`, `deps`, `tsconfig`.** Every
  compiler option is the tsconfig's, read through `tsgo --showConfig`, and
  every emit knob is a build flag: `--//ts:declarations=tsgo|oxc` (who emits
  the `.d.ts`), `--//ts:source_map` (default on), `--//ts:declaration_map`
  (default off), beside `--//ts:lib_check`. Deleted, with the macro in
  `ts/defs.bzl` that took them: `target`, `jsx_mode`, `jsx_import_source`,
  `lib`, `types`, `compiler_options`, `declarations`, `enable_check`,
  `source_map`, `declaration_map`, `tsgo_args`, `path_aliases`,
  `path_alias_srcs`, `types_srcs`, `vite_types`, `module_name`,
  `public_globals`, `untyped_packages`. `ts_test` loses the same compile-side
  parameters and keeps `tsconfig`; the test's tsconfig names `vitest/globals`
  in `types`.
  `ts_codegen` loses `module_name`; an `outs` codegen provides `TsInfo` over
  its outs, so a generated `.d.ts` is a dep whose consumer names it in `types`.
  `TsModuleInfo` and the declaration provider's `global_entry_files` are gone;
  `ts_dev_server` writes no `resolve.alias` for a first-party package.

  Migration: move each option into the package's tsconfig (`compilerOptions`
  `target`, `jsx`, `jsxImportSource`, `lib`, `types`, `paths`, `checkJs`,
  `typeRoots`, ...) and name the file in `tsconfig`. A `paths` value is read
  from the directory of the file that sets it and resolves to the source tree
  and to its bazel-bin twin, so a dep's declarations and a `ts_codegen`
  `out_dir` tree are reached through the alias the tsconfig already has. A global a consumer needs is named in the consumer's
  `types` (`"./worker-configuration.d.ts"`, `"../src/env.d.ts"`) with the
  owning target in `deps`; a bare specifier naming a workspace member resolves
  through the hub's view of it, `@npm//:<name>`. A package that leaked a global
  script into a program stops doing so when that program's `types` is written,
  because `types` is always written (the direct `@types/*` deps when the
  tsconfig sets none). Build a target under oxc's emit with
  `--//ts:declarations=oxc`; a test that has to is wrapped in a transition.
