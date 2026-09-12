### Changed

- **The Go tools are toolchains whose binaries are the tools release's assets;
  a consumer compiles no Go.** `//ts/toolchain:all` registers two new types
  over the four platforms of `TSGO_PLATFORMS`: `tools_toolchain_type`, exec-
  bound, `ToolsInfo(tsaction, lcov_merger, copy_to_workspace)`, and
  `launcher_toolchain_type`, target-bound as `js_runtime_type` is,
  `LauncherInfo(launcher)`; each instance's binaries come from
  `@tools_<platform>`, the `ts` extension's download of
  `rules_typescript-tools-<N>-<platform>.tar.gz` from the `tools-v<N>` release
  `ts/private/tools_lock.bzl` names, verified against the table's SRI. Every
  action that ran `//ts/tools/tsaction` reads the tools toolchain; `ts_test`'s
  `_lcov_merger` names `//ts/toolchain:lcov_merger_resolved`; `ts_test`,
  `ts_binary`, `ts_dev_server` and `npm_bin` are symlinks of the launcher
  toolchain's binary and fail analysis naming a target platform no launcher
  covers. The ruleset's own workspace and the nested integration workspaces
  register the source-built instances, `//ts/tools/tsaction:source_toolchain`
  and `//tools/launcher:all`, ahead of `//ts/toolchain:all`; `e2e/basic`,
  `examples/*` and the BCR presubmit download the release. `//tools/release
  -- tools <N>` tags a tools release, `release.yml` builds, checks, attaches and
  attests its assets, `ci.yml` rebuilds them on every PR against the table and
  gates the tools' sources on the tag. Gazelle is the one Go a consumer
  compiles, under `bazel run` alone. A consumer's `MODULE.bazel` is unchanged.
