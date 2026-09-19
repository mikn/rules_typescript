### Changed

- **A `tsconfig.json` under a `package.json` the lockfile has no importer for
  gets no target.** pnpm installs nothing for such a project, so a bare import
  in it resolved through whatever an importer above hoisted, or not at all,
  and the `ts_compile` and `ts_config` Gazelle wrote there were a package the
  workspace never installed. The directory is a foreign project, down to the
  next `package.json` that is an importer: no `tsconfig.json` under it is
  listed or is a package, its files are no src of the package above, a stale
  rule there is withdrawn with the reason, and one line names the manifest.
  The `--traceResolution` run over such a project and its list of unresolved
  specifiers go with it. A project meant to build is listed in
  `pnpm-workspace.yaml`.
