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

The generated sources are their own `ts_compile` target; see
[Compiling the output](#compiling-the-output).

A `ts_codegen` is hand-written. Gazelle reads every one in the BUILD files it
walks and never writes or rewrites one: a `ts_codegen` in a package's BUILD
file is a dep of every target Gazelle writes there, a file its `outs` declare
resolves to it wherever a program reaches the file, and everything under a
declared `out_dir` is the target's output whether or not a local run of the
generator left a copy on disk, so nothing under it is a source and an import
into the tree resolves to the target. A checked-in `*.gen.ts` no rule declares
is an ordinary source, listed by its program like any other; one the program's
`exclude` names is not.

## Compiling the Output

A generated file lives in the output tree, and one tsgo declaration emit has one
`rootDir`. A target holding generated sources alone hangs off one root, the
package's directory in `bazel-bin`, and builds under either emitter. Checked-in
and generated sources in one target hang off two, and fail at analysis under the
default emit:

```
ts_compile: srcs on @@//src/app:app hang off 2 different roots, and one
declaration emit has one rootDir:
  bazel-out/k8-fastbuild/bin/src/app
  src/app
```

Put the generated sources in their own target and depend on it, as above.
`--//ts:declarations=oxc` lifts the error for the whole build: oxc groups its
sources by root and runs once per group, and the check is a validation action
that reads any layout.

## Attributes

| Attribute | Type | Default | Description |
|-----------|------|---------|-------------|
| `srcs` | `label_list` | required | The files the generator reads. Empty is an analysis-time error |
| `outs` | `output_list` | `[]` | The files the generator writes, declared. The generator must write exactly these |
| `out_dir` | `string` | `""` | A single declared **directory** instead, for a generator that produces a tree it will not enumerate (Prisma's client, say) |
| `generator` | `label` | required | The executable, built for the exec configuration |
| `args` | `string_list` | `[]` | The generator's command line, after placeholder substitution |
| `node_modules` | `label` | `None` | The importer's [`node_modules`](node-modules.md) target, for a generator that imports npm packages at runtime |
| `env` | `string_dict` | `{}` | Extra environment for the action |

`outs` and `out_dir` are **mutually exclusive, and exactly one is required**.
Both being unset and both being set are separate analysis-time errors. Bazel
requires every output to be declared at analysis time, so a generator whose
output set depends on its input is only expressible as `out_dir`.

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
    node_modules = ":node_modules",
)

ts_compile(
    name = "app",
    srcs = ["main.ts"],
    tsconfig = "tsconfig.json",
    deps = [":messages"],
)
```

`main.ts` imports `#app/messages`, which the tsconfig's `paths` sends into the
tree (`"#app/messages": ["./compiled/index"]`); the rule writes a bazel-bin twin
of every `paths` value, so the entry reaches the tree the action wrote, and the
declarations inside it type the import.

The tree goes in `deps`, never in `srcs`. `srcs` declares one output per input
file at analysis time, and a directory has no file list until its action has
run; a directory in `srcs` is an analysis-time error naming the attribute. The
generator has to emit compiled output, `.js` beside `.d.ts`; nothing downstream
compiles the tree. A generator that emits `.ts` sources into a tree has no
route today.

A `paths` entry is the only way to import out of the tree by name. Without one
the tree is still staged for the consumer's type-check, but nothing points at it
and the import does not resolve:

```
error TS2307: Cannot find module '#app/messages' or its corresponding type
declarations.
```

A relative import into the tree, `./compiled/messages/greeting.js` from a source
in the same package, needs no entry. The undeclared-import check resolves it
against the directory, so it still names the label when the tree arrives only
through another dep.

Gazelle writes the `deps` entry for either spelling. An `out_dir` target is
indexed by the workspace-relative `out_dir` path a relative or aliased
specifier reaches it by; a specifier under that root resolves to the target. The
root is matched as a prefix, after every indexed source has failed to claim the
specifier. An `outs` target is indexed under no root: its `TsInfo.js` is
empty, so nothing depends on it for a module, and its outputs are importable
through the `ts_compile` that names it in `srcs`.

## Cloudflare Worker Bindings

`wrangler types` turns the bindings a worker reads off `env`, declared in its
wrangler config, into an `Env` interface plus the runtime's own globals
(`Request`, `Response`, `KVNamespace` and the rest) for the config's
compatibility date. The ruleset ships that command as a generator,
`@rules_typescript//tools/codegen:wrangler_types`, so the declaration is a build
output and no `worker-configuration.d.ts` is checked in:

```python
load("@rules_typescript//npm:defs.bzl", "node_modules")
load("@rules_typescript//ts:defs.bzl", "ts_codegen")

node_modules(
    name = "node_modules",
    deps = ["@npm//:wrangler"],
)

ts_codegen(
    name = "worker_types",
    srcs = ["wrangler.jsonc"],
    outs = ["worker-configuration.d.ts"],
    args = [
        "--config",
        "wrangler.jsonc",
        "--out",
        "{out}",
        "--srcs",
        "{srcs}",
        "--strict-vars=false",
    ],
    generator = "@rules_typescript//tools/codegen:wrangler_types",
    node_modules = ":node_modules",
    visibility = ["//visibility:public"],
)
```

The generator takes `--config <basename>`, `--out {out}` and `--srcs {srcs}`,
then the rest of the `wrangler types` command line as written:
`--strict-vars=false` types `vars` as `string` rather than their literal values,
`--env-interface CloudflareBindings` renames the interface,
`--include-runtime=false` leaves out the runtime half for a program that takes
it from `@cloudflare/workers-types` (the two are the same declarations, and a
program holding both gets a duplicate identifier for each), `--env staging`
picks one environment's bindings. The config is the one src it reads; adding
the file `main` names to `srcs` puts `Cloudflare.GlobalProps.mainModule` in the
output and changes nothing else. `build` is removed from the staged copy, at the
top level and under every `env`: `wrangler types` runs `build.command` before it
resolves `main` and drops the entry when the command fails, so nothing the
config names runs in the action and the output is the one a config without the
block gives. The runtime half comes from booting the `workerd` in
`node_modules` over loopback: measured with wrangler 4.126.0 in the Bazel
sandbox, it needs no network and no `CLOUDFLARE_API_TOKEN`, and two runs over
one config are byte-identical. A worker typed against the output has `lib`
without DOM and no `@cloudflare/workers-types` in `deps`.

The output has no top-level import or export, so what it declares is global. A
tsconfig names it in `compilerOptions.types` as `./worker-configuration.d.ts`,
and the rule rebases the entry to the staged file. Gazelle finds the file among
this target's `outs` and puts this target, the dep that stages it, in the `deps`
of every target under that tsconfig. Those targets sit in packages of their own,
so the `visibility` has to reach them. See
[a declaration the tsconfig names](../gazelle/overview.md#a-declaration-the-tsconfig-names);
`//tests/worker_types` is the worked example; its codegen is `env_types`, since
`worker_types` is the directory's name and so the `ts_compile`'s.

## Placeholders in `args`

Substituted into each argument string before the action runs. All paths are
execroot-relative.

| Placeholder | Expands to |
|---|---|
| `{srcs_dir}` | the directory of the **first** src |
| `{srcs}` | every src path, space-separated in one argument |
| `{out}` | the path of the first declared output; the `out_dir` directory when `out_dir` is set |
| `{outs_dir}` | the directory of the first declared output |
| `{node_modules_dir}` | the importer's `node_modules` directory; only substituted when `node_modules` is set |

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
| `NODE_PATH` | `node_modules` is set | the directory, for CJS resolution |
| `TS_CODEGEN_NODE_MODULES` | `node_modules` is set | the same path, for a script that forks a child process |

The directory is the importer's `node_modules`, so a generator's bare ESM
import resolves by Node's walk up from the script and a CJS one through
`NODE_PATH` alike; every link and every store tree the links reach is an input
of the action.

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
