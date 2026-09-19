### Breaking — ts_lint

- **The linter is configured once, by `ts.lint()` in the root `MODULE.bazel`,
  and every `ts_compile` and `ts_test` runs it as a validation action; the
  `ts_lint` rule is gone.** `ts.lint(binary, config, fail_on_warnings)` on the
  `ts` module extension writes the `@lint_config` repository, whose one
  `lint_config` target the `@rules_typescript//ts:lint` label flag names; each
  compile runs `TsLint` over its TypeScript, JavaScript and declaration srcs
  through `tsaction stamp` into `_validation`, with `--config` when the call
  names a file and `--max-warnings=0` under `fail_on_warnings` -- the spelling
  oxlint and eslint share, so there is no `linter` kind to choose. Deleted:
  `ts_lint` with `srcs`, `linter`, `linter_binary`, `config` and
  `fail_on_warnings`; `TsLintInfo` and its `//ts:defs.bzl` export; Gazelle's
  detection of `oxlint.json`, `.oxlintrc*`, `eslint.config.*` and `.eslintrc*`,
  the `<name>_lint` it wrote beside each `ts_compile` and the lockfile gate
  that refused a config whose linter the lockfile lacks; `docs/rules/ts-lint.md`
  (now `docs/guides/lint.md`).

  Migration: delete every `ts_lint`; add
  `ts.lint(binary = "@npm//:oxlint_bin", config = "//:oxlint.json")` to the
  root `MODULE.bazel`, with `exports_files` on the config; a
  `fail_on_warnings = True` on any `ts_lint` becomes the call's.
