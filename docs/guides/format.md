# Format validation

Configure the workspace's formatter once with `ts.format()` in `MODULE.bazel`. Every `ts_compile` and `ts_test` validates its checked-in `srcs`, including JSON, styles and documentation listed there. Generated outputs are excluded. The `TsFormat` stamp belongs to `_validation`, so a formatting failure fails the build without becoming a dependent's compilation prerequisite.

```python
ts.format(
    binary = "@npm//:oxfmt_bin",
    config = "//:.oxfmtrc.jsonc",
    args = ["--list-different", "--no-error-on-unmatched-pattern"],
)
```

Export the config from its BUILD package. `data` declares files the config reads, such as imported styles. The binary and its runfiles come from the chosen lockfile. With no call, formatting is disabled. Only the root module configures the formatter.

`args` defaults to `["--check"]`; `--config` and the selected source paths are appended as absolute paths beneath the action’s staged workspace root. The shared action runner copies the checked-in inputs into a workspace-shaped directory. A tool that changes a copied input fails before creating the stamp; formatting never repairs the checkout as a build side effect. Config, source, data and tool changes invalidate the action. Formatting does not consume compiler outputs or type information.

For files outside TypeScript programs, use `ts_format(name = "format", srcs = [...])` from `@rules_typescript//ts:defs.bzl`. It uses the same root configuration. Select the files explicitly in BUILD files, including files in other Bazel packages through exported labels or filegroups. Enabling the extension does not automatically cover files absent from the build graph.

Run `bazel build //:format` for that explicit set, or build the compile/test targets normally. `--norun_validations` skips formatting with other validations; `--@rules_typescript//ts:format=@rules_typescript//ts:no_format` disables formatting on compile/test targets alone. Use checkout's formatter command to apply fixes.
