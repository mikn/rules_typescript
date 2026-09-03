### Fixed

- **Gazelle now resolves an import of an `out_dir` `ts_codegen` tree to the
  target that generates it.** Only `ts_compile` and `ts_test` were indexed as a
  dependency source, so a standalone `ts_codegen` answered to no specifier.
  Every import of a module inside its tree fell off the end of the ladder
  unresolved and got no dep, and the undeclared-import check then failed the
  consumer with the label Gazelle had declined to write. The dep had to be
  added by hand and pinned with `# keep`, once per target, each time a new
  target imported the tree. An `out_dir` target is now indexed by the roots its
  modules sit under: its `module_name`, and the workspace-relative `out_dir`
  path that a relative or path-aliased specifier reaches it by. The tree's
  contents do not exist until its action has run, so a specifier is matched
  against the root as a prefix, in a key namespace only these trees occupy, and
  only after exact resolution has missed. A rule indexing the same path under
  its own name is out of reach of the match, and an indexed source of the same
  name still wins. An `outs` `ts_codegen` stays unindexed: it returns no
  `JsInfo`, so no dep on it resolves, and its outputs reach importers through
  the `ts_compile` that names it in `srcs`.
