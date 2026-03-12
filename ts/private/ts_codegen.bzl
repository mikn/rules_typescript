"""General-purpose code generation rule for TypeScript projects.

ts_codegen runs an executable generator that reads source files and produces
generated TypeScript source files as Bazel action outputs.

It is the TypeScript equivalent of proto_library → go_proto_library:
  - Source files go in (via srcs)
  - Generated .ts files come out (via outs or out_dir)
  - An executable generator transforms the sources

Output declaration:
  - outs: explicit list of output file names (use for single-file or known multi-file output)
  - out_dir: directory name for generators that produce a file tree (e.g. Prisma)

The generator binary is run as a Bazel build action (not at analysis time), so:
  - All outputs are declared at analysis time via the outs attr
  - The action is fully hermetic and cacheable
  - Generated files can be fed directly into ts_compile as srcs

Typical patterns:

  1. Shell script generator (simplest):
     Wrap a shell script with sh_binary and pass it as generator.

         sh_binary(name = "mygen", srcs = ["mygen.sh"])
         ts_codegen(
             name = "gen",
             srcs = ["input.ts"],
             outs = ["generated.ts"],
             generator = ":mygen",
             args = ["--out", "{out}"],
         )

  2. Node.js generator with npm imports:
     Wrap a Node.js script that imports from node_modules with sh_binary.
     The shell wrapper invokes node directly (node is in PATH via toolchain).

         sh_binary(name = "gen_routes", srcs = ["generate-routes.sh"])
         ts_codegen(
             name = "route_tree",
             srcs = glob(["src/routes/**/*.tsx"]),
             outs = ["src/routeTree.gen.ts"],
             generator = ":gen_routes",
             args = ["--routes-dir", "{srcs_dir}", "--out", "{out}"],
             node_modules = ":node_modules",
         )

     The shell wrapper receives NODE_PATH and TS_CODEGEN_NODE_MODULES env
     variables automatically when node_modules is set, enabling Node.js
     to find npm packages.

Placeholder substitution in args:
  {srcs_dir}         → execroot-relative directory of the first src file
  {out}              → execroot-relative path of the first declared output
  {outs_dir}         → execroot-relative directory of the first declared output
  {srcs}             → space-separated list of all src file paths
  {node_modules_dir} → execroot-relative path of the node_modules directory
                       (only valid when node_modules is set)

When node_modules is set, ts_codegen automatically sets:
  NODE_PATH              → node_modules directory (for Node.js CJS resolution)
  TS_CODEGEN_NODE_MODULES → same path (for scripts that fork child processes)
"""

load("//ts/private:runtime.bzl", "JS_RUNTIME_TOOLCHAIN_TYPE", "get_js_runtime")

# ─── Rule implementation ───────────────────────────────────────────────────────

def _ts_codegen_impl(ctx):
    # Collect all source files.
    srcs = ctx.files.srcs
    if not srcs:
        fail("ts_codegen: srcs must not be empty")

    # Validate that exactly one of outs or out_dir is set.
    has_outs = len(ctx.outputs.outs) > 0
    has_out_dir = ctx.attr.out_dir != ""
    if not has_outs and not has_out_dir:
        fail("ts_codegen: either outs or out_dir must be set")
    if has_outs and has_out_dir:
        fail("ts_codegen: outs and out_dir are mutually exclusive; set exactly one")

    # Collect declared output files (or declare a directory).
    if has_out_dir:
        # Directory output: declare a single directory for generators that
        # produce a tree of files (e.g. Prisma client generation).
        out_dir_file = ctx.actions.declare_directory(ctx.attr.out_dir)
        outs = [out_dir_file]
    else:
        outs = ctx.outputs.outs

    # Compute placeholder values.
    # {srcs_dir}: directory of the first source file (execroot-relative).
    srcs_dir = srcs[0].dirname if srcs else ""

    # {out}: path of the first output file (execroot-relative).
    out_path = outs[0].path if outs else ""

    # {outs_dir}: directory of the first output file.
    outs_dir = outs[0].dirname if outs else ""

    # {srcs}: space-separated list of all source paths.
    srcs_list = " ".join([f.path for f in srcs])

    # Collect node_modules files and compute the node_modules directory path.
    node_modules_files = []
    node_modules_dir = ""
    if ctx.attr.node_modules:
        node_modules_files = ctx.files.node_modules
        if node_modules_files:
            first_nm = node_modules_files[0]
            if first_nm.is_directory:
                node_modules_dir = first_nm.path
            else:
                # Fallback: use the parent of the first file.
                node_modules_dir = first_nm.dirname

    # Resolve the JS runtime from the toolchain (for passing NODE_BINARY env).
    js_runtime = get_js_runtime(ctx)
    runtime_binary = None
    if js_runtime:
        runtime_binary = js_runtime.runtime_binary

    # Expand placeholders in each argument string.
    expanded_args = []
    for arg in ctx.attr.args:
        a = arg
        a = a.replace("{srcs_dir}", srcs_dir)
        a = a.replace("{out}", out_path)
        a = a.replace("{outs_dir}", outs_dir)
        a = a.replace("{srcs}", srcs_list)
        if node_modules_dir:
            a = a.replace("{node_modules_dir}", node_modules_dir)
        expanded_args.append(a)

    # Build the action environment.
    action_env = {}
    action_env.update(ctx.attr.env)

    # When node_modules is provided, automatically set NODE_PATH so that the
    # generator script can import npm packages without knowing their path.
    # Also expose the path via TS_CODEGEN_NODE_MODULES for scripts that fork.
    if node_modules_dir:
        action_env["NODE_PATH"] = node_modules_dir
        action_env["TS_CODEGEN_NODE_MODULES"] = node_modules_dir

    # When a JS runtime is available from the toolchain, expose its path via
    # NODE_BINARY so generator shell scripts can invoke `$NODE_BINARY script.mjs`
    # without relying on `node` being in PATH.
    extra_inputs = []
    if runtime_binary:
        action_env.setdefault("NODE_BINARY", runtime_binary.path)
        extra_inputs.append(runtime_binary)

    # Build the full input depset: srcs + node_modules + runtime (if any).
    inputs = depset(srcs + node_modules_files + extra_inputs)

    # Run the generator action.
    ctx.actions.run(
        executable = ctx.executable.generator,
        arguments = expanded_args,
        inputs = inputs,
        outputs = outs,
        env = action_env,
        mnemonic = "TsCodegen",
        progress_message = "TsCodegen %{label}",
    )

    return [
        DefaultInfo(files = depset(outs)),
    ]

# ─── Rule declaration ──────────────────────────────────────────────────────────

ts_codegen = rule(
    implementation = _ts_codegen_impl,
    attrs = {
        "srcs": attr.label_list(
            doc = "Source files read by the generator (e.g. route files, schema files).",
            allow_files = True,
            mandatory = True,
        ),
        "outs": attr.output_list(
            doc = """Declared output files produced by the generator.

All outputs must be declared at analysis time — Bazel requires this.
The generator must write exactly the files listed here.

Mutually exclusive with out_dir. Use out_dir when the generator
produces a directory tree instead of individual files.
""",
        ),
        "out_dir": attr.string(
            doc = """Output directory name for generators that produce a tree of files.

When set, a single Bazel declared directory is created at this path.
The generator is expected to write all of its outputs under this directory.

Use this for generators like Prisma that produce many files in a tree
(e.g. generated/client/index.ts, generated/client/schema.prisma, ...).

Mutually exclusive with outs. The {out} and {outs_dir} placeholders in
args resolve to the declared directory path when out_dir is used.
""",
            default = "",
        ),
        "generator": attr.label(
            doc = """Executable that produces the generated files.

Typically an sh_binary wrapping a shell script or a Node.js script.
Built for the exec configuration (build machine) so it runs as a Bazel action.

When the generator is a Node.js script that imports npm packages, wrap it in
sh_binary and rely on the NODE_BINARY env variable (set automatically by
ts_codegen when the js_runtime toolchain is registered) to invoke node:

    #!/usr/bin/env bash
    exec "$NODE_BINARY" "$0.runfiles/_main/path/to/script.mjs" "$@"
""",
            mandatory = True,
            executable = True,
            cfg = "exec",
        ),
        "args": attr.string_list(
            doc = """Command-line arguments passed to the generator.

Supports placeholder substitution:
  {srcs_dir}         → execroot-relative directory of the first src file
  {out}              → execroot-relative path of the first declared output file
  {outs_dir}         → execroot-relative directory of the first declared output
  {srcs}             → space-separated list of all src file paths
  {node_modules_dir} → execroot-relative path of the node_modules directory
                       (only valid when node_modules is set)

Example:
    args = ["--routes-dir", "{srcs_dir}", "--out", "{out}"]
""",
            default = [],
        ),
        "node_modules": attr.label(
            doc = """Optional node_modules target providing npm packages for the generator.

When set:
  - The node_modules tree is added to the action's inputs
  - NODE_PATH is set to the node_modules directory (for CJS resolution)
  - TS_CODEGEN_NODE_MODULES is set to the same path
  - {node_modules_dir} placeholder is available in args

Use this when the generator script imports npm packages at runtime.
""",
            allow_files = True,
        ),
        "env": attr.string_dict(
            doc = "Additional environment variables passed to the generator action.",
            default = {},
        ),
    },
    toolchains = [
        config_common.toolchain_type(JS_RUNTIME_TOOLCHAIN_TYPE, mandatory = False),
    ],
    doc = """Runs a generator executable to produce TypeScript source files from inputs.

ts_codegen is the TypeScript equivalent of proto_library -> go_proto_library:
it runs an executable generator that reads source files and produces generated
.ts files which can then be compiled with ts_compile.

See module docstring for invocation patterns and placeholder substitution.
""",
)
