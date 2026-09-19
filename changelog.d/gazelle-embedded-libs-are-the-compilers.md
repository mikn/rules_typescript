### Fixed

- **Gazelle under the source-built compiler reports no lib as unowned.** The
  binary `//ts/toolchain/tsgo_source` builds embeds its `lib.*.d.ts` and lists
  them under `bundled:///libs/`, where the lockfile's binary lists them under
  `../`; Gazelle took the first shape for first-party files and printed every
  lib a program reached as a file no package owns, once per run, burying the
  reports about files. A path under `bundled:///` is the compiler's own, as
  one under `../` is: no package, no report. No BUILD file moves: a lib's
  reasons write no edge.
