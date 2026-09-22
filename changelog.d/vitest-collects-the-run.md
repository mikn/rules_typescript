### Changed

- **vitest collects a `ts_test`'s run by its own glob.** The launcher passed
  every compiled test file on the command line and the generated config
  listed each in `test.include`; vitest matched the list's patterns over the
  runfiles tree and then every file it found against every argument, 52 s
  before `Start at` over 2,235 files. The generated config's include is now
  `**/*.{test,spec}.{js,jsx,mjs,cjs}`, vitest's default over the compiled
  extensions, set after the merge on the root and on every project -- a
  config's `include`, written for the sources, is not read -- and the
  launcher names no file.
