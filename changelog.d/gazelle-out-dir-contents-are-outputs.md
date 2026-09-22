### Fixed

- **Gazelle no longer lists what sits under a `ts_codegen` `out_dir` as
  sources.** A local run of the generator leaves its output on disk under
  `out_dir`, and the generator read the tree as checked-in sources: every
  generated `.d.ts` went into the package's `ts_compile.srcs`, over files the
  `ts_codegen` already declares as its output. Every file under a declared
  `out_dir` is that target's output, on disk or not: nothing under it is a
  src, a directory at or below an `out_dir` gets no package, and a BUILD file
  an earlier run left there is emptied and named in the log, since Gazelle
  cannot delete it. The `out_dir` is read from the `ts_codegen` rules in the
  BUILD files walked.
