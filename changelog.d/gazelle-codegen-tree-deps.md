### Fixed

- **Gazelle now resolves an import of an `out_dir` `ts_codegen` tree to the
  target that generates it.** Only `ts_compile` and `ts_test` were indexed as a
  dependency source, so a standalone `ts_codegen` answered to no specifier at
  all: every import of a module inside its tree fell off the end of the ladder
  unresolved and got no dep, and the undeclared-import check then failed the
  consumer with the very label Gazelle had just declined to write. The dep had
  to be added by hand and pinned with `# keep`, once per target — which is only
  discovered when a *new* target imports the tree and the build breaks. An
  `out_dir` target is now indexed by the roots its modules sit under: its
  `module_name`, and the workspace-relative `out_dir` path that a relative or
  path-aliased specifier reaches it by. The tree's contents do not exist until
  its action has run, so a specifier is matched against the root as a prefix,
  in a key namespace only these trees occupy — a rule indexing the same path
  under its own name is out of reach of the match — and only after exact
  resolution has missed, so an indexed source of the same name still wins. An
  `outs` `ts_codegen` stays unindexed: it returns no `JsInfo`, so no dep on it
  resolves, and its outputs reach importers through the `ts_compile` that names
  it in `srcs`.
