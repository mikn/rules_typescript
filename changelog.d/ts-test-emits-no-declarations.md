### Changed

- **A `ts_test`'s program emits no declarations, so its srcs may hang off
  several roots.** `compile_program` declared a `.d.ts` per src for every
  caller, so a test's program stood under the one-`rootDir` rule of the
  declaration emit, and a src of another package -- `exports_files` where it
  lives, the label in `srcs` under `# keep` -- failed analysis: `ts_compile:
  srcs on @@//web:web_test hang off 2 different roots, and one declaration
  emit has one rootDir`. Nothing reads a test's declarations. `ts_test` now
  asks `compile_program` for none: no `.d.ts` output, no `TsgoDeclare`, no
  `declarations` output group, no `isolatedDeclarations` in its check under
  `--//ts:declarations=oxc`; `TsgoCheck` is its one tsgo action and `TsEmit`
  runs oxc once per root. The compiled module of a src outside the test's
  package is held in the runfiles at the src's own path too, where the
  relative import from a compiled sibling reaches it, and is the module's id
  under vitest. `//tests/vitest/cross_package` pins it.
