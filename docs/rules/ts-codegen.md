# ts_codegen

Runs a generator executable that reads sources and writes TypeScript, as a
declared build action. It is the TypeScript equivalent of
`proto_library` → `go_proto_library`: a `ts_compile` takes declared files in
`srcs`, and a declared directory (`out_dir`) in `deps`.

```python
load("@rules_typescript//ts:defs.bzl", "ts_binary", "ts_codegen", "ts_compile")

ts_binary(
    name = "gen_routes",
    entry_point = "generate-routes.mjs",
)

ts_codegen(
    name = "route_tree",
    srcs = glob(["src/routes/**/*.tsx"]),
    outs = ["src/routeTree.gen.ts"],
    args = ["--routes-dir", "{srcs_dir}", "--out", "{out}"],
    generator = ":gen_routes",
)

ts_compile(
    name = "route_tree_ts",
    srcs = [":route_tree"],
    declarations = "oxc",
)

ts_compile(
    name = "app",
    srcs = ["src/main.tsx"],
    deps = [":route_tree_ts"],
)
```

`ts_binary` runs the `.mjs` on the JS runtime toolchain, so the generator is the
script and nothing else. A generator that imports npm packages at runtime
additionally takes `node_modules`; see
[The environment the generator gets](#the-environment-the-generator-gets).

The generated sources are their own `ts_compile` target, and it does not use the
default declaration emit; see [Compiling the output](#compiling-the-output).

Gazelle detects Prisma, GraphQL codegen and OpenAPI generators from the files
in a directory (`schema.prisma`; `.graphql`/`.gql` sources beside a
`codegen.ts`/`.yml`/`.yaml`/`.json`; an `openapi.*` or `swagger.*` spec) when
the lockfile has the tool, and writes the target. The `# gazelle:ts_codegen`
directive registers a generator it does not recognise; see
[Register a codegen target](../gazelle/directives.md#register-a-codegen-target).
It writes the `ts_compile` that consumes the output too, named
`<name>_compile`, and resolves imports of the generated module to it. A
checked-in file a `ts_codegen` declares as an out is kept out of the package's
`srcs`, and so is everything under a declared `out_dir`: the tree is the
target's output whether or not a local run of the generator left a copy on
disk. A checked-in `*.gen.ts` no rule declares is an ordinary source.
`routeTree.gen.ts` is the exception: the Start Vite plugin writes it.
[`# gazelle:ts_exclude`](../gazelle/directives.md#exclude-generated-files)
takes one out of `srcs`.

## Compiling the Output

A generated file lives in the output tree, and the default emit
(`declarations = "tsgo"`, `enable_check = True`) cannot take it from there. Two
failures:

**Checked-in and generated sources in one target fail at analysis.** One tsgo
declaration emit has one `rootDir`, and those two sets hang off different roots:

```
ts_compile: srcs on @@//src/app:app hang off 2 different roots, and one
declaration emit has one rootDir:
  bazel-out/k8-fastbuild/bin/src/app
  src/app
```

**Generated sources alone fail inside the tsgo action.** Under that emit
`outDir` is the package's directory in `bazel-bin`, which is where the generated
source already is, and TypeScript's implicit `exclude` covers `outDir`, so the
program comes out empty:

```
error TS18003: No inputs were found in config file
'.../route_tree_ts.tsconfig.json'. Specified 'include' paths were
'["src/routeTree.gen.ts"]' ...
```

The target holding generated sources picks another emitter:

- `declarations = "oxc"`: oxc emits the `.d.ts` syntactically, per file, with
  no type program, and downstream targets type-check against the generated
  code. Every export needs an explicit type.
- `enable_check = False`: no type program and no `.d.ts`, for generated code
  whose types nothing downstream consumes. `//tests/codegen` uses it.

`declarations = "oxc"` also lifts the first error, so one target holding both
sets is expressible: oxc groups its sources by root and runs once per group.

## Attributes

| Attribute | Type | Default | Description |
|-----------|------|---------|-------------|
| `srcs` | `label_list` | required | The files the generator reads. Empty is an analysis-time error |
| `outs` | `output_list` | `[]` | The files the generator writes, declared. The generator must write exactly these |
| `out_dir` | `string` | `""` | A single declared **directory** instead, for a generator that produces a tree it will not enumerate (Prisma's client, say) |
| `generator` | `label` | required | The executable, built for the exec configuration |
| `args` | `string_list` | `[]` | The generator's command line, after placeholder substitution |
| `node_modules` | `label` | `None` | An npm tree for a generator that imports packages at runtime. Name the target `node_modules` if the generator uses ESM |
| `module_name` | `string` | `""` | The bare specifier the `out_dir` tree is importable as. Requires `out_dir` |
| `env` | `string_dict` | `{}` | Extra environment for the action |

`outs` and `out_dir` are **mutually exclusive, and exactly one is required**.
Both being unset and both being set are separate analysis-time errors. Bazel
requires every output to be declared at analysis time, so a generator whose
output set depends on its input is only expressible as `out_dir`.

`module_name` requires `out_dir`; see [A directory of output](#a-directory-of-output).

## A Directory of Output

A generator whose file names come from its input (one module per message
bundle, per Prisma model, per GraphQL operation) cannot have its outputs
declared. `out_dir` declares the directory instead, and the target carries the
providers a `ts_compile` reads a dep through:

```python
ts_codegen(
    name = "messages",
    srcs = ["project.inlang/settings.json"] + glob(["messages/*.json"]),
    out_dir = "compiled",
    args = ["--project", "{srcs_dir}", "--outdir", "{out}"],
    generator = ":compile_messages",
    module_name = "#app/messages",
    node_modules = ":node_modules",
)

ts_compile(
    name = "app",
    srcs = ["main.ts"],
    deps = [":messages"],
)
```

`main.ts` imports `#app/messages`, and the declarations inside the tree type it.

The tree goes in `deps`, never in `srcs`. `srcs` declares one output per input
file at analysis time, and a directory has no file list until its action has
run; a directory in `srcs` is an analysis-time error naming the attribute. The
generator has to emit compiled output, `.js` beside `.d.ts`; nothing downstream
compiles the tree. A generator that emits `.ts` sources into a tree has no
route today.

`module_name` is the only way to import out of the tree by name. Without it the
tree is still staged for the consumer's type-check, but no `paths` entry points
at it and the import does not resolve:

```
error TS2307: Cannot find module '#app/messages' or its corresponding type
declarations.
```

A relative import into the tree, `./compiled/messages/greeting.js` from a source
in the same package, needs no `module_name`. The undeclared-import check
resolves it against the directory, so it still names the label when the tree
arrives only through another dep.

Gazelle writes the `deps` entry for either spelling. An `out_dir` target is
indexed by the roots its modules sit under: its `module_name`, and the
workspace-relative `out_dir` path a relative or aliased specifier reaches it by.
A specifier under one of those roots resolves to the target. The root is
matched as a prefix, after every indexed source has failed to claim the
specifier. An `outs` target is indexed under no root: it returns no `JsInfo`,
so nothing depends on it, and its outputs are importable through the
`ts_compile` that names it in `srcs`.

## Checking the Output In

Some generated files have to be in the source tree. A route tree is one: the
routes are typed against it, and one `ts_compile` cannot hold both it and them.
`refresh_workspace_files` copies build outputs into the workspace under
`bazel run`, and a `diff_test` beside it fails when the checked-in copy drifts.
`examples/tanstack-app/src/routes/BUILD.bazel` is the worked example, built in
CI; abridged here to the two rules:

```python
load("@bazel_skylib//rules:diff_test.bzl", "diff_test")
load("@rules_typescript//ts:defs.bzl", "refresh_workspace_files", "ts_codegen")

ts_codegen(
    name = "route_tree",
    srcs = glob(["**/*.tsx"]),
    outs = ["routeTree.gen.expected.ts"],  # keep
    args = ["--out", "{out}", "--srcs", "{srcs}"],
    generator = "@rules_typescript//tools/codegen:tanstack_routes",
    node_modules = "//:router_generator_node_modules",
)

refresh_workspace_files(
    name = "update_route_tree",
    files = {":route_tree": "src/routes/routeTree.gen.ts"},
)

diff_test(
    name = "route_tree_test",
    size = "small",
    failure_message = "src/routes/routeTree.gen.ts is stale: run `bazel run //src/routes:update_route_tree`.",
    file1 = ":route_tree",
    file2 = "routeTree.gen.ts",
)
```

`bazel run //src/routes:update_route_tree` writes the file and prints
`wrote src/routes/routeTree.gen.ts`. The declared out carries a name of its
own, since an output named after a source file in the same package is a Bazel
error; the copy takes the checked-in name. The checked-in file stays in the
`ts_compile`'s `srcs` with a `# keep`, and `# keep` on `outs` holds the entry
Gazelle did not write.

| Attribute | Type | Default | Description |
|-----------|------|---------|-------------|
| `files` | `label_keyed_string_dict` | required | Each key is a target producing exactly one file; the value is where it is written, relative to the workspace root |

A key producing another count fails at analysis:
`refresh_workspace_files: @@//src/routes:route_tree produces 2 files, want exactly one.`

The rule decides what to write at analysis time and writes a manifest; the copy
is a Go binary reading `BUILD_WORKSPACE_DIRECTORY`, so the target runs only
under `bazel run` and refuses a destination outside the workspace
(`copy_to_workspace: destination "../x" is outside the workspace`).
`bazel run //:refresh_tsconfig` is an instance of this rule: `ts_refresh_tsconfig`
declares one over the generated tsconfig, the hook data and the tsserver plugin
files, and the nested editor tsconfigs reach the copier through a private
provider the generator returns.

## Placeholders in `args`

Substituted into each argument string before the action runs. All paths are
execroot-relative.

| Placeholder | Expands to |
|---|---|
| `{srcs_dir}` | the directory of the **first** src |
| `{srcs}` | every src path, space-separated in one argument |
| `{out}` | the path of the first declared output; the `out_dir` directory when `out_dir` is set |
| `{outs_dir}` | the directory of the first declared output |
| `{node_modules_dir}` | the npm tree's path; only substituted when `node_modules` is set |

`{srcs_dir}` and `{outs_dir}` are the first entry's directory, not a common
ancestor. A `glob()` spanning two directories hands the generator one of them; a
generator that needs the whole set takes `{srcs}`.

`{srcs}` becomes a single argument containing every path, space-separated, so a
generator taking a list needs a shell wrapper that word-splits it.

## The Environment the Generator Gets

The rule sets three variables:

| Variable | When | Value |
|---|---|---|
| `NODE_BINARY` | a `js_tool` toolchain is registered | the toolchain node. Set with `setdefault`, so an `env` entry of your own wins |
| `NODE_PATH` | `node_modules` is set | the tree's directory, for CJS resolution |
| `TS_CODEGEN_NODE_MODULES` | `node_modules` is set | the same path, for a script that forks a child process |

A generator with bare ESM imports needs the target named `node_modules`. The
tree's directory is named after its target, and Node's ESM resolver looks only
in a directory called `node_modules` as it walks up; `NODE_PATH` is a CJS
mechanism, and ESM ignores it. `node_modules = ":codegen_node_modules"` fails at
runtime:

```
Error [ERR_MODULE_NOT_FOUND]: Cannot find package 'consola'
```

One `node_modules` target per package follows; a second npm tree in the same
package can only serve a CJS generator.

A Node generator is a [`ts_binary`](ts-binary.md) whose `entry_point` is the
script. The rule resolves the runtime from the JS runtime toolchain and locates
the entry through the runfiles library; `node` need not be on `PATH`, and the
script need not read `NODE_BINARY`:

```python
ts_binary(
    name = "gen_schema",
    entry_point = "generate-schema.mjs",
    data = ["schema-helpers.mjs"],
)
```

Sibling modules the entry imports go in `data`; that puts them in runfiles
beside it.

`NODE_BINARY` still reaches the generator's environment, for a generator that
forks a child Node process of its own.

A generator that is not a Node program (a Go binary, a Rust binary, a shell
script) is any executable target, `sh_binary` included. `sh_binary` is a
`rules_shell` rule, not a built-in, and needs its own `load`; a BUILD file
without the line fails with `name 'sh_binary' is not defined`. Locate a script
inside a shell wrapper with the Bash runfiles library and `rlocation`;
`"$0.runfiles"` does not exist when Bazel hands the action a runfiles manifest
instead of a tree:

```bash
#!/usr/bin/env bash
# source @bazel_tools//tools/bash/runfiles first; it defines rlocation.
exec "$NODE_BINARY" "$(rlocation _main/path/to/script.mjs)" "$@"
```
