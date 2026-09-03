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

`wrangler types` turns the bindings a worker reads off `env`, declared in
`wrangler.jsonc`, into an `Env` interface, plus the runtime's own globals
(`Request`, `Response`, `KVNamespace` and the rest) for the config's
compatibility date. `ts_worker_types` runs the command as a build action, so the
declarations follow the config on every build.

## Inputs

The config, the wrangler in `node_modules`, and nothing else. `wrangler types`
reads the bindings out of the config and generates the runtime half by booting
the `workerd` that ships in `node_modules` over loopback; measured with wrangler
4.126.0 in the Bazel sandbox, it needs no network and no
`CLOUDFLARE_API_TOKEN`, and two runs over one config are byte-identical.

`srcs` is optional. wrangler reads no binding from the sources; it writes
`Cloudflare.GlobalProps.mainModule` as `typeof import` of the file `main` names.
With `main` in `srcs` that block is in the output; without it, it is not. The
two runs otherwise differ in nothing but the hash in the header.

## wrangler Flags

`wrangler_args` is the rest of the `wrangler types` command line, verbatim:

| Flag | Effect |
|------|--------|
| `--strict-vars=false` | `vars` typed as `string`; the default is their literal values |
| `--env-interface CloudflareBindings` | the global interface named `CloudflareBindings`; the default is `Env` |
| `--include-runtime=false` | the `Env` half only, for a program that gets the runtime from `@cloudflare/workers-types` |
| `--env staging` | the bindings of one environment |

The runtime half and `@cloudflare/workers-types` are the same declarations; a
program holding both gets a duplicate identifier for each one. A worker typed
against the generated file has `lib` without DOM and no
`@cloudflare/workers-types` in `deps`. A worker that takes the package passes
`--include-runtime=false`.

## Output

The output has no top-level import or export, so what it declares is global. It
reaches a program through a relative `compilerOptions.types` entry, with the
label that stages the file in
[`types_srcs`](ts-compile.md#a-types-entry-that-names-a-declaration-file). The
generated config writes the entry as the path to the file, in `bazel-out` for a
build output; the pair is written the same way for a checked-in file.
`types_srcs` travels on no dep edge, so a consumer outside the worker gets none
of its globals.

Gazelle writes that pair onto every target under the tsconfig, and names this
target, not a filegroup, when the entry names a file this target writes. See
[a declaration the tsconfig
names](../gazelle/overview.md#a-declaration-the-tsconfig-names).

## The Editor

The generated file is in `bazel-out`, which the root editor program excludes.
The package's own editor program
([`nested_tsconfigs`](../getting-started/ide-setup.md#nested-tsconfigs), which
a target with a `types` entry of its own gets) writes the entry through the
`bazel-bin` symlink. `bazel build` puts the declarations where the editor reads
them.

## Hermeticity

`wrangler types` writes beside the config it read and resolves `main` relative
to it; a Bazel output directory is read-only. The config and the staged `srcs`
are copied into a scratch directory at their paths relative to the config, with
a `node_modules` link beside them, and the declarations are copied to `out`
afterwards. The header comment wrangler writes echoes its argv, so the output
path is left at wrangler's default and `-c` is passed only for a config wrangler
would not find by name.

Two `ts_worker_types` in one package need two different `out` names. So does a
package that holds a checked-in `worker-configuration.d.ts`; otherwise Bazel
reports the generated file as overlapping the source one.

## Attributes

| Attribute | Description |
|-----------|-------------|
| `name` | Name of the target. |
| `config` | The wrangler config. The declarations name its bindings; the runtime half is generated for its compatibility date. |
| `node_modules` | A `node_modules()` target carrying wrangler. |
| `srcs` | Worker sources to stage beside the config; `main` among them puts `GlobalProps.mainModule` in the output. |
| `wrangler_args` | `wrangler types` flags, as written on its command line. |
| `out` | Name of the generated file. Defaults to `worker-configuration.d.ts`. |

`ts_worker_types` is a macro over [`ts_codegen`](ts-codegen.md), so `visibility`
and `tags` reach that target.
