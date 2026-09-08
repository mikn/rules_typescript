### Added

- **A `ts_test`'s runfiles hold its package's TypeScript sources at their
  source paths.** The test's own `srcs` and the srcs of every dep in the same
  Bazel package -- `.ts`, `.tsx` and declarations -- are staged beside the
  compiled `.js`, as every other file of the package already is, so a test that
  reads its package's tree (`readFileSync(new URL("./index.ts",
  import.meta.url))`) finds it where the checkout has it; another package's
  files reach a test through `data`. The compiled program is what runs: a
  `setupFiles` entry naming a source runs the compiled sibling a dep staged,
  whether or not the source is beside it. `TsInfo.sources` carries them.
