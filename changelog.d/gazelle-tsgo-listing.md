### Added

- **Gazelle lists every hand-written `tsconfig.json` through tsgo.** A run
  executes `tsgo -p <dir>/tsconfig.json --noEmit --listFilesOnly --explainFiles
  --pretty false` from the repository root for each directory holding one and
  keeps the answer: the files `include` and `files` matched, every import,
  `/// <reference>` and `types` edge with its importer and specifier, and the
  `compilerOptions.types` entries verbatim. Nothing reads the listing yet, so
  generated BUILD files are unchanged. A root `tsconfig.json` whose extends
  chain has neither `include` nor `files` is not listed, since tsgo would
  enumerate the whole repository. A program tsgo exits non-zero on keeps what
  tsgo listed, with the diagnostics on the program: `TS18003` is a program with
  no roots, and one tsgo lists nothing for is recorded as not listed with its
  diagnostic. A run prints none of them; `-ts_verbose` prints each under its
  `tsconfig.json`. A reason line the parser does not know stops the run with
  the line quoted. The binary is the toolchain's, carried in the Gazelle
  binary's runfiles; `-ts_tsgo=<path>` names another, and a run with no binary
  at all (a `go test` outside Bazel) says so once and goes on without the
  listing. `-ts_verbose` also prints one line per program and, once the walk
  is done, the `.ts`/`.tsx`/`.mts`/`.cts` files no program lists, per
  directory and in total.
