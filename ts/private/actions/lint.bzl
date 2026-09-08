"""Lint as one validation action per compile, configured once at the module.

The root module's ts.lint(binary, config, fail_on_warnings) writes the
@lint_config repository; its one target is what the //ts:lint label flag
names, and compile_program runs TsLint over the program's sources when it
names a binary. docs/guides/lint.md is the reference.
"""

LintConfigInfo = provider(
    doc = "What //ts:lint resolves to: the linter every compile runs.",
    fields = {
        "binary": "FilesToRunProvider, or None when nothing lints.",
        "config": "File passed as --config, or None.",
        "fail_on_warnings": "bool: --max-warnings=0 is passed.",
    },
)

def _lint_config_impl(ctx):
    binary = None
    if ctx.attr.binary:
        binary = ctx.attr.binary[DefaultInfo].files_to_run
    return [LintConfigInfo(
        binary = binary,
        config = ctx.file.config,
        fail_on_warnings = ctx.attr.fail_on_warnings,
    )]

lint_config = rule(
    implementation = _lint_config_impl,
    attrs = {
        "binary": attr.label(
            doc = "The linter's executable, `@npm//:oxlint_bin` or " +
                  "`@npm//:eslint_bin`; unset, no target lints.",
            executable = True,
            cfg = "exec",
        ),
        "config": attr.label(
            doc = "The linter's own config file, passed as --config; " +
                  "unset, the flag is not passed.",
            allow_single_file = True,
        ),
        "fail_on_warnings": attr.bool(
            doc = "A warning fails the build: --max-warnings=0, which " +
                  "oxlint and eslint spell alike.",
        ),
    },
    doc = "The linter //ts:lint names; ts.lint() writes the one at " +
          "@lint_config//:lint.",
)

def _lint_config_repo_impl(repository_ctx):
    attrs = repository_ctx.attr
    lines = ['    name = "lint",']
    if attrs.binary:
        lines.append('    binary = "{}",'.format(attrs.binary))
    if attrs.config:
        lines.append('    config = "{}",'.format(attrs.config))
    lines.append("    fail_on_warnings = {},".format(attrs.fail_on_warnings))
    lines.append('    visibility = ["//visibility:public"],')
    repository_ctx.file(
        "BUILD.bazel",
        'load("{}", "lint_config")\n\nlint_config(\n{}\n)\n'.format(
            str(Label("//ts/private/actions:lint.bzl")),
            "\n".join(lines),
        ),
    )

lint_config_repo = repository_rule(
    implementation = _lint_config_repo_impl,
    attrs = {
        "binary": attr.string(
            doc = "The linter's label, canonical; \"\" for none.",
        ),
        "config": attr.string(
            doc = "The config file's label, canonical; \"\" for none.",
        ),
        "fail_on_warnings": attr.bool(),
    },
    doc = "The repository holding the lint_config a root module's ts.lint() " +
          "configured.",
)

# An npm bin launcher cds to its runfiles before exec, so a relative path
# would miss; tsaction substitutes its working directory for the token.
_FROM_EXECROOT = "{{EXECROOT}}/%s"

def lint_action(ctx, lint, srcs):
    """Registers TsLint over `srcs` under `lint`, which names a binary.

    Returns the stamp tsaction writes when the linter exits 0, for _validation.
    """
    stamp = ctx.actions.declare_file("{}.tslint".format(ctx.label.name))
    args = ctx.actions.args()
    args.use_param_file("@%s", use_always = False)
    args.set_param_file_format("multiline")
    args.add("-stamp", stamp)
    args.add("--")
    args.add(lint.binary.executable)
    inputs = list(srcs)
    if lint.config:
        args.add("--config", lint.config, format = _FROM_EXECROOT)
        inputs.append(lint.config)
    if lint.fail_on_warnings:
        args.add("--max-warnings=0")
    args.add_all(srcs, format_each = _FROM_EXECROOT)
    ctx.actions.run(
        inputs = inputs,
        tools = [lint.binary],
        outputs = [stamp],
        executable = ctx.executable._tsaction,
        arguments = ["stamp", args],
        env = {"PATH": "/bin:/usr/bin"},
        mnemonic = "TsLint",
        progress_message = "TsLint %{label}",
    )
    return stamp
