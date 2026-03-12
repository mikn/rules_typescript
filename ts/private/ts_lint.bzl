"""Linting rule for TypeScript sources using oxlint or eslint.

ts_lint runs a linter (default: oxlint) as a Bazel validation action in the
_validation output group.  Like tsgo type-checking, linting:

  - Runs unconditionally during `bazel build` (or explicitly with
    `bazel build --output_groups=+_validation`).
  - Does NOT block downstream compilation — a lint error in a library does
    not prevent binaries that depend on it from being compiled.
  - Is fully cached: if the source files and config have not changed, Bazel
    skips the lint action.

Supported linters
-----------------
oxlint  (default)
    Fast Rust-based linter from the oxc project.  No config file required
    for basic use.  Pass a `config` label pointing to an oxlint.json
    (or .oxlintrc.json) file for custom rule configuration.

eslint
    The JavaScript ecosystem standard.  Requires an ESLint flat-config file
    (eslint.config.mjs or equivalent).  The node_modules tree must be wired
    in via `data` or by ensuring the linter binary target already depends on
    a node_modules() target.

Usage
-----
    load("@rules_typescript//ts:defs.bzl", "ts_compile", "ts_lint")

    ts_compile(
        name = "lib",
        srcs = ["index.ts", "utils.ts"],
    )

    ts_lint(
        name = "lib_lint",
        srcs = ["index.ts", "utils.ts"],
        # Optional: linter = "eslint" (default: "oxlint")
        # Optional: config = ":eslint.config.mjs"
    )

Action mnemonic: TsLint
"""

load("//ts/private:providers.bzl", "TsDeclarationInfo")

# ─── Provider ──────────────────────────────────────────────────────────────────

TsLintInfo = provider(
    doc = "Provider returned by ts_lint rules.",
    fields = {
        "stamp": "File: The validation stamp produced on a clean lint run.",
    },
)

# ─── Rule implementation ────────────────────────────────────────────────────────

def _ts_lint_impl(ctx):
    srcs = ctx.files.srcs
    if not srcs:
        fail(
            "ts_lint: 'srcs' must be non-empty. " +
            "Add at least one .ts or .tsx file to the srcs attribute. " +
            "Example: srcs = [\"index.ts\", \"utils.ts\"]",
        )

    linter = ctx.attr.linter
    if linter not in ("oxlint", "eslint"):
        fail("ts_lint: 'linter' must be 'oxlint' or 'eslint'; got '{}'.  " +
             "See the rule documentation for supported linters.".format(linter))

    # Resolve the linter binary.  Both ctx.executable.linter_binary and the
    # default oxlint/eslint targets use the same attribute; the rule defaults
    # differ based on `linter`.
    linter_bin = ctx.executable.linter_binary

    # Config file is optional — pass --config only when provided.
    config_file = ctx.file.config

    # The stamp file is Bazel's concrete output that tracks linting results.
    stamp = ctx.actions.declare_file("{}.tslint".format(ctx.label.name))

    # Build the command.  We use run_shell so we can chain the touch command.
    #
    # oxlint invocation:
    #   oxlint [--config <cfg>] <src> ...
    #
    # eslint invocation:
    #   eslint [--config <cfg>] <src> ...
    #
    # Both exit non-zero on lint errors; the shell command propagates that exit
    # code and Bazel fails the action accordingly.
    src_paths = " ".join(['"{}"'.format(f.path) for f in srcs])

    if config_file:
        config_flag = '--config "{}"'.format(config_file.path)
    else:
        config_flag = ""

    # oxlint needs --deny-warnings when we want lint warnings to fail the build.
    # We keep the default non-strict behaviour (warnings are informational) to
    # match typical developer expectations.  Users can set `fail_on_warnings`
    # attr to change this.
    warnings_flag = "--deny-warnings" if ctx.attr.fail_on_warnings else ""

    cmd = """\
set -euo pipefail
"{linter_bin}" {config_flag} {warnings_flag} {srcs}
/bin/touch "{stamp}"
""".format(
        linter_bin = linter_bin.path,
        config_flag = config_flag,
        warnings_flag = warnings_flag,
        srcs = src_paths,
        stamp = stamp.path,
    )

    inputs = list(srcs)
    if config_file:
        inputs.append(config_file)

    ctx.actions.run_shell(
        inputs = depset(inputs + [linter_bin]),
        outputs = [stamp],
        command = cmd,
        env = {"PATH": "/bin:/usr/bin"},
        mnemonic = "TsLint",
        progress_message = "TsLint %{label}",
    )

    return [
        DefaultInfo(files = depset([stamp])),
        OutputGroupInfo(_validation = depset([stamp])),
        TsLintInfo(stamp = stamp),
    ]

# ─── Rule declaration ──────────────────────────────────────────────────────────

ts_lint = rule(
    implementation = _ts_lint_impl,
    attrs = {
        "srcs": attr.label_list(
            doc = "TypeScript source files to lint (.ts, .tsx).",
            allow_files = [".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs"],
            mandatory = True,
        ),
        "linter": attr.string(
            doc = "Linter to use: 'oxlint' (default, fast Rust-based) or 'eslint'.",
            default = "oxlint",
            values = ["oxlint", "eslint"],
        ),
        "linter_binary": attr.label(
            doc = """Label of the linter executable.

For oxlint: an @npm//:oxlint_bin target (from npm_translate_lock) or a
filegroup wrapping an oxlint binary.

For eslint: an @npm//:eslint_bin target or similar.

If not specified, the rule will fail with a helpful message asking you to
provide the binary label.  There is no toolchain for linters because they are
typically managed via the project's own package.json rather than a separate
Bazel toolchain.
""",
            mandatory = True,
            executable = True,
            cfg = "exec",
        ),
        "config": attr.label(
            doc = "Optional linter configuration file (oxlint.json, .oxlintrc.json, eslint.config.mjs, etc.).",
            allow_single_file = True,
        ),
        "fail_on_warnings": attr.bool(
            doc = "When True, warnings are treated as errors (passes --deny-warnings to oxlint). Default False.",
            default = False,
        ),
    },
    doc = """Runs a linter (oxlint or eslint) as a Bazel validation action.

The lint check is placed in the _validation output group, which Bazel runs
unconditionally during `bazel build` but does NOT block downstream compilation.
This means lint errors are reported immediately without preventing the rest of
the build from proceeding.

To run only linting (e.g. in CI):

    bazel build //... --output_groups=+_validation

To disable linting for a specific target temporarily, add:

    tags = ["no-lint"]

and wrap the rule in a conditional (see Bazel docs on tags).

Example with oxlint (no config):

    ts_lint(
        name = "my_lib_lint",
        srcs = ["index.ts"],
        linter_binary = "@npm//:oxlint_bin",
    )

Example with eslint and a flat config:

    ts_lint(
        name = "my_lib_lint",
        srcs = ["index.ts"],
        linter = "eslint",
        linter_binary = "@npm//:eslint_bin",
        config = "//:eslint.config.mjs",
    )
""",
)
