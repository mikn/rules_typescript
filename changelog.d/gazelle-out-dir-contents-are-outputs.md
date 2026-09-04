### Fixed

- **Gazelle no longer lists what sits under a `ts_codegen` `out_dir` as
  sources.** A local run of the generator leaves its output on disk under
  `out_dir`, and the tsconfig-mode rollup read the tree as checked-in sources:
  every generated `.d.ts` went into the package's `ts_compile.srcs`, and the
  generator's README got an `asset_library`, all over files the `ts_codegen`
  already declares as its output. In every-dir mode each directory below the
  `out_dir` holding a source got a BUILD file of its own. Every file under a
  declared `out_dir` is that target's output, on disk or not. The rollup no
  longer enters the directory, and a directory at or below an `out_dir` gets
  no package in either mode; a BUILD file an earlier run left there is emptied
  and named in the log, since Gazelle cannot delete it. The `out_dir` is read
  from `# gazelle:ts_codegen ... dir:` directives, from the generators Gazelle
  detects, and from `ts_codegen` rules in the BUILD files walked, so a
  hand-written target counts.
