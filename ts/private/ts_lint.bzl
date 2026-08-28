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

# How each supported linter spells "a warning is an error".
_WARNINGS_AS_ERRORS = {
    "oxlint": "--deny-warnings",
    "eslint": "--max-warnings=0",
}

# The tsaction helper substitutes this with the action's working directory. An
# npm_bin linter wrapper cds to RUNFILES_DIR, which invalidates every
# execroot-relative path it was handed.
_EXECROOT = "{{EXECROOT}}"

def _execroot_path(f):
    return _EXECROOT + "/" + f.path

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

    linter_bin = ctx.executable.linter_binary

    # Config file is optional — pass --config only when provided.
    config_file = ctx.file.config

    stamp = ctx.actions.declare_file("{}.tslint".format(ctx.label.name))

    args = ctx.actions.args()
    args.add("-stamp", stamp)
    args.add("--")
    args.add(linter_bin)
    if config_file:
        args.add("--config", _execroot_path(config_file))
    if ctx.attr.fail_on_warnings:
        args.add(_WARNINGS_AS_ERRORS[ctx.attr.linter])
    args.add_all(srcs, map_each = _execroot_path)
    args.use_param_file("@%s", use_always = False)
    args.set_param_file_format("multiline")

    inputs = list(srcs)
    if config_file:
        inputs.append(config_file)

    # An npm_bin wrapper locates Node and its package's native binary through
    # its own runfiles, which only a FilesToRunProvider in `tools` stages.
    linter_files_to_run = ctx.attr.linter_binary[DefaultInfo].files_to_run

    ctx.actions.run(
        inputs = depset(inputs),
        tools = [linter_files_to_run],
        outputs = [stamp],
        executable = ctx.executable._tsaction,
        arguments = ["stamp", args],
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
            doc = "Linter whose CLI is being driven: 'oxlint' (default, fast Rust-based) or 'eslint'. " +
                  "It selects the spelling of the flags ts_lint passes, not the binary — that is linter_binary.",
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
            doc = "When True, warnings fail the build: --deny-warnings for oxlint, --max-warnings=0 for eslint. Default False.",
            default = False,
        ),
        "_tsaction": attr.label(
            default = Label("//ts/tools/tsaction"),
            executable = True,
            cfg = "exec",
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
