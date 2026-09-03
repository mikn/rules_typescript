# ts_worker_types

Generates a worker's `worker-configuration.d.ts` from its wrangler config.

```python
load("@rules_typescript//npm:defs.bzl", "node_modules")
load("@rules_typescript//ts:defs.bzl", "ts_compile", "ts_config", "ts_worker_types")

node_modules(
    name = "node_modules",
    deps = ["@npm//:wrangler"],
)

ts_worker_types(
    name = "worker_types",
    config = "wrangler.jsonc",
    node_modules = ":node_modules",
    wrangler_args = ["--strict-vars=false"],
)

ts_config(
    name = "tsconfig",
    src = "tsconfig.json",
)
```

```python
# src/BUILD.bazel -- what Gazelle writes under a tsconfig that names the file
ts_compile(
    name = "src",
    srcs = ["index.ts"],
    tsconfig = "//workers/api:tsconfig",
    types = ["../worker-configuration.d.ts"],
    types_srcs = ["//workers/api:worker_types"],
)
```

The bindings a worker reads off `env` are declared in `wrangler.jsonc`, and
`wrangler types` is what turns them into an `Env` interface -- plus the
runtime's own globals, `Request`, `Response`, `KVNamespace` and the rest,
generated for the config's compatibility date. A worker repo checks the file in
and re-runs the command by hand, with a hook or a CI step diffing the result to
catch drift. This runs the command as a build action instead: the declarations
follow the config on every build, and the drift gate has nothing to check.

## Inputs

The config, the wrangler in `node_modules`, and nothing else. `wrangler types`
reads the bindings out of the config and generates the runtime half by booting
the `workerd` that ships in `node_modules` over loopback; measured with wrangler
4.126.0 in the Bazel sandbox, it needs no network and no
`CLOUDFLARE_API_TOKEN`, and two runs over one config are byte-identical.

`srcs` is optional. wrangler does not read the worker's sources for a binding;
what it does with the file `main` names is write
`Cloudflare.GlobalProps.mainModule` as `typeof import` of it. Stage `main` in
`srcs` and that block is in the output; leave it out and it is not -- the two
runs otherwise differ in nothing but the hash in the header.

## wrangler flags

`wrangler_args` is the rest of the `wrangler types` command line, verbatim:

| Flag | Effect |
|------|--------|
| `--strict-vars=false` | `vars` typed as `string` rather than as their literal values |
| `--env-interface CloudflareBindings` | the global interface under that name rather than `Env` |
| `--include-runtime=false` | the `Env` half only, for a program that gets the runtime from `@cloudflare/workers-types` |
| `--env staging` | the bindings of one environment |

The runtime half and `@cloudflare/workers-types` are the same declarations, and
a program holding both gets a duplicate identifier for each one. A worker typed
against the generated file -- `lib` without DOM, no `@cloudflare/workers-types`
in `deps` -- is the shape `wrangler types` writes for; a worker that already
takes the package passes `--include-runtime=false`.

## The output is an ambient .d.ts

What `wrangler types` writes has no top-level import or export, so what it
declares is global. It reaches a program the way a tsconfig names it: a
relative `compilerOptions.types` entry, with the label that stages the file in
[`types_srcs`](ts-compile.md#a-types-entry-that-names-a-declaration-file). The
generated config writes the entry as the path to the file, which for a build
output is in `bazel-out`; nothing about the pair is different for a checked-in
file. `types_srcs` travels on no dep edge, so a consumer outside the worker gets
none of its globals.

Gazelle writes that pair onto every target under the tsconfig, naming this
target rather than a filegroup when the file the entry names is one this target
writes -- see [a declaration the tsconfig
names](../gazelle/overview.md#a-declaration-the-tsconfig-names).

## The editor

The generated file is in `bazel-out`, which the root editor program excludes.
The package's own editor program -- [`nested_tsconfigs`](../getting-started/ide-setup.md#nested-tsconfigs),
which a target with a `types` entry of its own gets anyway -- writes the entry
through the `bazel-bin` symlink, so `bazel build` is what puts the declarations
where the editor reads them.

## Hermeticity

`wrangler types` writes beside the config it read, resolves `main` relative to
it, and a Bazel output directory is read-only. So the config and the staged
`srcs` are copied into a scratch directory at their paths relative to the
config, with a `node_modules` link beside them, and the declarations are copied
to `out` afterwards. The header comment wrangler writes echoes its own argv, so
the output path is left at wrangler's default and `-c` is passed only for a
config wrangler would not find by its name: the header reads as a hand run's
would.

Two `ts_worker_types` in one package need two different `out` names. So does a
package that still holds a checked-in `worker-configuration.d.ts` -- otherwise
Bazel reports the generated file as overlapping the source one, which is the
right answer: two files of that name are two answers to one question.

## Attributes

| Attribute | Description |
|-----------|-------------|
| `name` | Name of the target. |
| `config` | The wrangler config. Its bindings are what the declarations name, and its compatibility date is what the runtime half is generated for. |
| `node_modules` | A `node_modules()` target carrying wrangler. |
| `srcs` | Worker sources to stage beside the config; `main` among them puts `GlobalProps.mainModule` in the output. |
| `wrangler_args` | `wrangler types` flags, as written on its command line. |
| `out` | Name of the generated file. Defaults to `worker-configuration.d.ts`. |

`ts_worker_types` is a macro over [`ts_codegen`](ts-codegen.md), so `visibility`
and `tags` reach that target.
