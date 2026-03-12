"""Type-checking rule using tsgo as a Bazel validation action.

ts_check runs `tsgo --project tsconfig.json --noEmit` against a generated
tsconfig as a validation action.  Validation actions are placed in the
`_validation` output group, which Bazel runs unconditionally during
`bazel build` but does NOT block downstream compilation.  This means:

  - Type errors are reported on `bazel build` (they don't silently disappear).
  - Downstream targets are NOT blocked by a type error in a leaf target.
  - tsgo operates on .d.ts files from deps (not .ts source from deps), proving
    that the compilation boundary is the .d.ts artifact.

Note: `tsgo --build` (composite / project-references mode) is intentionally
NOT used because TypeScript does not allow `composite: true` with
`noEmit: true`.  Using `--project ... --noEmit` avoids that conflict
entirely while still providing accurate type-checking.

Action mnemonic: TsgoCheck
"""

load("//ts/private:providers.bzl", "TsConfigInfo", "TsDeclarationInfo")
load("//ts/private:toolchain.bzl", "TSGO_TOOLCHAIN_TYPE", "get_tsgo_toolchain")

# ─── Rule implementation ───────────────────────────────────────────────────────

def _ts_check_impl(ctx):
    tsgo = get_tsgo_toolchain(ctx)

    # The tsconfig to check against.
    tsconfig_info = ctx.attr.tsconfig[TsConfigInfo]
    tsconfig_file = tsconfig_info.tsconfig

    # Collect all transitive .d.ts files from deps.  tsgo needs these for
    # type resolution but we do NOT include the .ts sources from deps.
    dep_dts_sets = []
    for dep in ctx.attr.deps:
        if TsDeclarationInfo in dep:
            dep_dts_sets.append(dep[TsDeclarationInfo].transitive_declaration_files)

    dep_dts_depset = depset(transitive = dep_dts_sets, order = "postorder")

    # Collect transitive tsconfig files (project references must be on disk for
    # tsgo to resolve them).
    dep_tsconfigs = tsconfig_info.deps_tsconfigs

    # The validation output is a sentinel stamp file.  tsgo produces no
    # actionable artifact, so we write a stamp file after a successful run
    # to give Bazel a concrete output to track.
    validation_out = ctx.actions.declare_file(
        "{}.tscheck_validation".format(ctx.label.name),
    )

    # Source files for this target that tsgo will type-check.
    srcs = ctx.files.srcs

    # Build the action using run_shell so we can append the touch command.
    # The shell command runs tsgo --project <tsconfig> --noEmit and on success
    # writes the stamp.  Any non-zero exit code from tsgo propagates as a
    # build failure.
    #
    # env PATH is set explicitly so that /bin/touch is found even under
    # --incompatible_strict_action_env (which clears PATH).
    cmd = """\
set -euo pipefail
"{tsgo_bin}" --project "{tsconfig}" --noEmit
/bin/touch "{stamp}"
""".format(
        tsgo_bin = tsgo.tsgo_binary.path,
        tsconfig = tsconfig_file.path,
        stamp = validation_out.path,
    )

    ctx.actions.run_shell(
        inputs = depset(
            srcs + [tsconfig_file, tsgo.tsgo_binary],
            transitive = [dep_dts_depset, dep_tsconfigs],
        ),
        outputs = [validation_out],
        command = cmd,
        env = {"PATH": "/bin:/usr/bin"},
        mnemonic = "TsgoCheck",
        progress_message = "TsgoCheck %{label}",
    )

    return [
        DefaultInfo(files = depset([validation_out])),
        OutputGroupInfo(
            _validation = depset([validation_out]),
        ),
    ]

# ─── Rule declaration ──────────────────────────────────────────────────────────

ts_check = rule(
    implementation = _ts_check_impl,
    attrs = {
        "srcs": attr.label_list(
            doc = "TypeScript source files for this target (used as direct action inputs).",
            allow_files = [".ts", ".tsx"],
        ),
        "deps": attr.label_list(
            doc = "ts_compile targets whose .d.ts outputs feed into the type-check.",
            providers = [[TsDeclarationInfo]],
        ),
        "tsconfig": attr.label(
            doc = "A ts_config_gen target providing the generated tsconfig.json.",
            providers = [TsConfigInfo],
            mandatory = True,
        ),
    },
    toolchains = [TSGO_TOOLCHAIN_TYPE],
    doc = """Runs tsgo type-checking as a Bazel validation action.

Uses `tsgo --project tsconfig.json --noEmit` (not --build mode) so that
composite: true is not required in the tsconfig, avoiding the TypeScript
restriction that composite and noEmit cannot be combined.

Placed in the _validation output group so it runs unconditionally during
`bazel build` without blocking downstream compilation.

Example:
    ts_check(
        name = "button_check",
        srcs = ["Button.tsx"],
        deps = ["//components/icon"],
        tsconfig = ":button_tsconfig",
    )
""",
)
