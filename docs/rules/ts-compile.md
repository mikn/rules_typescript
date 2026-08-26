# ts_compile

Compiles TypeScript with Oxc and emits `.d.ts` declarations with tsgo.

## Usage

```python
load("@rules_typescript//ts:defs.bzl", "ts_compile")

ts_compile(
    name = "lib",
    srcs = ["index.ts", "math.ts"],
    deps = ["//other/package", "@npm//:zod"],
    visibility = ["//visibility:public"],
)
```

That is the whole setup. Unmodified TypeScript compiles: no explicit export type
annotations, no tsconfig wiring, no extra flags in `.bazelrc`.

## Attributes

| Attribute | Type | Default | Description |
|-----------|------|---------|-------------|
| `srcs` | `label_list` | required | `.ts`, `.tsx`, `.d.ts`, `.js`, `.mjs` or `.cjs` files. A JavaScript src is staged unchanged and joins the type program — the rule sets `allowJs` for it, so its JSDoc types cross the package boundary; add `checkJs` through `compiler_options` to have its own body checked. A `.jsx` file is rejected at analysis time: oxc has no output extension for one, and the message says to rename it `.tsx` |
| `deps` | `label_list` | `[]` | `ts_compile`, `ts_npm_package`, `css_library`, `css_module`, `asset_library` or `json_library` targets |
| `target` | `string` | `"es2022"` | ECMAScript target version |
| `jsx_mode` | `string` | `"react-jsx"` | JSX transform: `react-jsx`, `react`, `preserve`; empty disables JSX |
| `declarations` | `string` | `"tsgo"` | Which tool emits `.d.ts`: `"tsgo"` or `"oxc"` |
| `enable_check` | `bool` | `True` | Run tsgo. See [Turning tsgo off](#turning-tsgo-off) |
| `source_map` | `bool` | `True` | Emit a `.js.map` next to every `.js`. Off for a target whose JavaScript nothing debugs — a codegen step, or a bundle input whose bundler makes its own map |
| `declaration_map` | `bool` | `False` | Emit a `.d.ts.map`, so go-to-definition across a package boundary lands on the `.ts` source. Requires the tsgo emit (`declarations = "tsgo"` with `enable_check = True`); oxc emits no map |
| `tsgo_args` | `string_list` | `[]` | Extra tsgo flags, restricted to the ones that only report on the program: `--traceResolution`, `--explainFiles`, `--listFiles`, `--listEmittedFiles`, `--diagnostics`, `--extendedDiagnostics`, `--noErrorTruncation` |
| `tsconfig` | `label` | `None` | The project's own `tsconfig.json`, or a [`ts_config`](#ts_config) target, as the `compilerOptions` baseline |
| `lib` | `string_list` | `None` | `compilerOptions.lib`, e.g. `["es2022", "webworker"]`. Replaces the whole set `target` implies — this is how DOM gets dropped from a worker build |
| `types` | `string_list` | `None` | `compilerOptions.types` — which ambient type packages load. `[]` loads none. Relative entries resolve against this target's package |
| `jsx_import_source` | `string` | `None` | `compilerOptions.jsxImportSource`, e.g. `"solid-js"`, `"preact"` |
| `compiler_options` | `dict` | `None` | Any other `compilerOptions`, passed through verbatim |
| `module_name` | `string` | `""` | The bare specifier this target is importable as, e.g. `"@acme/ui"` |
| `path_aliases` | `string_dict` | `{}` | Alias prefix → workspace-relative **source** directory. Must resolve to files this target stages: its own `srcs`, or `path_alias_srcs` |
| `path_alias_srcs` | `label_list` | `[]` | The files a `path_aliases` entry resolves to when they are not in `srcs`. They join this target's type program, so a type error in one of them fails this target |
| `vite_types` | `bool` | `False` | Prepend the Vite ambient type shim to `srcs` |

## Outputs

For each source file `foo.ts`:

| Output | Description |
|--------|-------------|
| `foo.js` | Compiled JavaScript (always from Oxc) |
| `foo.js.map` | Source map |
| `foo.d.ts` | Declaration file — the compilation boundary |

Under `declarations = "tsgo"` with `enable_check = False` there is no type
program and therefore no `.d.ts` at all; see
[Turning tsgo off](#turning-tsgo-off).

## Where compiler options come from

The generated tsconfig is a hybrid, because it carries machinery the rules must
own: `rootDirs` bridging the source and output trees, `paths` for npm packages,
the `files` list that carries each `@types/*` dep, and under
`declarations = "tsgo"` the
`outDir`/`rootDir`/`noEmitOnError` triple. A user tsconfig that replaced those
would silently break module resolution. So a user tsconfig supplies the
baseline and the generated one `extends` it.

Lowest precedence first:

1. **`tsconfig`** — the project's own `tsconfig.json`, and whatever it extends.
   Referenced where it lives, never copied, so relative paths inside it still
   resolve against the directory they were written for. When it is set the
   ruleset adds no compiler opinions of its own, so tsgo checks the code under
   the same options `tsc` does.
2. **The zero-config baseline** — `strict`, `module: "Preserve"`,
   `moduleResolution: "Bundler"`, `skipLibCheck`, `esModuleInterop`. Applied
   **only** when `tsconfig` is unset. With a `tsconfig`, these come from your
   file or from tsc's own defaults if the file omits them.
3. **`target` and `jsx_mode`**, then `jsx_import_source`, `lib`, `types`, then
   `compiler_options`, which wins among these.
4. **The options Bazel owns** — `paths`, `include`, and the 16 keys listed
   below.

`target` and `jsx_mode` are injected in *every* mode, including over a tsconfig
baseline, and supersede a `target` or `jsx` in the file: oxc transforms with
them and the two compilers disagreeing is worse than deferring to the file.

One option rides in unasked between layers 1 and 2: `allowArbitraryExtensions`,
which the `.d.ts` files generated for `css_module` / `css_library` /
`asset_library` / `json_library` deps require. It overrides the value in your
tsconfig file; to get your own value back, set it in `compiler_options`, which
sits above it.

### The two hard errors

Both fail at analysis time rather than misbehaving quietly.

**A Bazel-owned key in `compiler_options`.** These 16 encode the sandbox layout
or the action's declared outputs, so a user value breaks the build instead of
configuring it:

`baseUrl`, `rootDirs`, `paths`, `outDir`, `rootDir`, `declarationDir`,
`declaration`, `emitDeclarationOnly`, `declarationMap`, `sourceMap`, `noEmit`,
`noEmitOnError`, `isolatedDeclarations`, `composite`, `incremental`,
`tsBuildInfoFile`.

`declarationMap` and `sourceMap` follow from the `declaration_map` and
`source_map` attributes; the rest name the attribute to use instead in the
failure message.

```
ts_compile: compilerOptions.paths is set by the rule and cannot be overridden --
use path_aliases for source aliases, or module_name on the target that produces
the declarations.
Remove "paths" from compiler_options on //src/app:app.
```

**A `path_aliases` value pointing into the output tree.** A path under
`bazel-out/` or `bazel-bin/` embeds the build configuration, so it breaks under
`-c opt` or a different exec platform:

```
ts_compile: path_aliases["@acme/ui"] on //src/app:app points into the output
tree (bazel-out/k8-fastbuild/bin/packages/ui).
```

The supported answer is `module_name` on the producing target.

### Generated and checked-in sources in one target

One tsgo declaration emit has one `rootDir`, and a checked-in source and a
generated one hang off different roots — the package directory and the package's
directory in `bazel-bin`. A target holding both fails at analysis under the
default emit, naming the roots:

```
ts_compile: srcs on //src/app:app hang off 2 different roots, and one
declaration emit has one rootDir:
  bazel-out/k8-fastbuild/bin/src/app
  src/app
```

Put the generated sources in their own target and depend on it, or set
`declarations = "oxc"` (which groups sources by root and runs oxc once per
group) or `enable_check = False`. Neither of the last two emits from tsgo, so
neither has a `rootDir` to be single. What the generated sources cannot do is
use the default emit even on their own — see
[ts_codegen § Compiling the output](ts-codegen.md#compiling-the-output).

## Deps have to be direct

A source may import only what a **direct** dep provides. Every `ts_compile`
target that has both sources and `deps` runs a `TsStrictDeps` action, which
reads those sources and fails on any specifier that resolves only because it
arrives through another dep's own deps:

```
ERROR: .../src/app/BUILD.bazel:3:11: TsStrictDeps //src/app:app failed: (Exit 1)
//src/app:app imports a module no direct dep provides:

  src/app/main.ts:1  imports "./hidden"
                     add "//src/app:hidden" to deps
  src/app/main.ts:2  imports "zod"
                     add "@npm//:zod" to deps

Each of those resolves today only because it reaches this target through
another dep's own deps, and stops resolving the moment that dep drops it.
Re-run gazelle to regenerate deps, or add the labels above by hand.
```

`bazel run //:gazelle` writes those labels. There is no flag and no opt-out:
the whole value is that a green build means the `deps` list is true.

**What is checked:** relative imports, bare specifiers that name an npm package,
and bare specifiers that name another target's `module_name` (including a
subpath of one). **What is exempt:** Node builtins and `node:` specifiers, and
anything under a `path_aliases` prefix — an alias resolves to files this target
already stages. An import that *nothing* in the closure provides is left alone,
because there is no label to suggest; TypeScript reports it as `TS2307`.

`/// <reference types="x" />` is not checked. It is a real resolution channel,
but Gazelle generates no dep for it either, and a check Gazelle cannot satisfy
would be a failure with no fix.

### Why the closure is still an action input

Resolution becomes direct; the inputs do not. One `paths` map serves the whole
type program, so dropping the transitive entries would stop a *declared* dep's
own `.d.ts` from resolving *its* imports — which TypeScript widens to `any`
rather than reporting. The split is made by the check, not by the tsconfig: the
transitive `.d.ts` stay available for the declarations that need them, and the
check is what refuses to let your own source lean on them.

### Importing another target by bare specifier

`path_aliases` maps a prefix to a *source* directory, which is right for
`@/components` → `src/components`. It cannot name another target's generated
declarations, because only that target knows where they land under the current
configuration. Set `module_name` there instead:

```python
# packages/ui/BUILD.bazel
ts_compile(
    name = "ui",
    srcs = glob(["*.ts", "*.tsx"]),
    module_name = "@acme/ui",
    visibility = ["//visibility:public"],
)

# apps/web/BUILD.bazel
ts_compile(
    name = "web",
    srcs = ["main.ts"],          # import { Button } from "@acme/ui";
    deps = ["//packages/ui"],
)
```

The dependent gets a `paths` entry for `@acme/ui` and `@acme/ui/*` pointing at
the `.d.ts` files Bazel produced, with `index.d.ts` as the entry point. The name
travels in the `TsModuleInfo` provider, transitively.

### ts_config

Starlark cannot read a file to follow its `extends` chain, so a tsconfig that
extends another file has to declare the chain. Every file in it becomes an input
to the type-check action:

```python
load("@rules_typescript//ts:defs.bzl", "ts_compile", "ts_config")

ts_config(
    name = "tsconfig",
    src = "tsconfig.json",
    deps = ["//:tsconfig.base.json"],
)

ts_compile(
    name = "lib",
    srcs = ["index.ts"],
    tsconfig = ":tsconfig",
)
```

A tsconfig that extends nothing goes straight into `ts_compile`:

```python
ts_compile(
    name = "lib",
    srcs = ["index.ts"],
    tsconfig = "tsconfig.json",
)
```

### Worked example: a Cloudflare Worker

Ambient globals from a generated `.d.ts`, no DOM, and no stray `@types` package
from the dependency graph reaching global scope:

```python
ts_compile(
    name = "worker",
    srcs = ["index.ts"],
    tsconfig = "tsconfig.json",
    lib = ["es2022"],
    types = ["./worker-configuration.d.ts"],
    compiler_options = {"resolveJsonModule": True},
)
```

Relative entries in `types` and `typeRoots` are rewritten to resolve from the
generated config, so they are written exactly as they would be in the package's
own tsconfig. Other path-valued options are *not* rewritten — they resolve
against the generated config's directory, so they belong in the `tsconfig` file
instead.

## Which tool emits the declarations

`declarations` is the one real trade-off in this rule. Oxc always does the
JavaScript transform; the attribute decides who produces the `.d.ts`.

### `declarations = "tsgo"` (default)

tsgo emits declarations from the complete type program — the target's sources
plus every transitive `.d.ts` and npm `package.json`. Consequences:

- **No source annotations required.** Inferred export types are fine.
- **Declarations are exactly what `tsc` would emit**, including inferred object
  shapes, literal unions and `RegExp`.
- **Type errors fail `bazel build`.** The `.d.ts` are real outputs of the tsgo
  action, so a target with a type error produces nothing and cannot hand a
  stale declaration to a consumer. No `--output_groups=+_validation` needed.
- **Type-checking is on the critical path.** A consumer waits for its
  dependency's declarations.

### `declarations = "oxc"`

Oxc emits declarations syntactically, per file, with no type program. This
requires [isolated declarations](../getting-started/isolated-declarations.md):
every export needs an explicit type, and Oxc **errors** when one does not have
one. In exchange, type-checking moves into the `_validation` output group, off
the critical path, so downstream targets compile while checking runs
concurrently.

Use it per package once that package's exports are annotated. It is a
throughput optimisation, not a requirement.

### Turning tsgo off

`enable_check = False` means different things in the two modes, because tsgo
plays a different role in each:

| | `enable_check = True` (default) | `enable_check = False` |
|---|---|---|
| `declarations = "tsgo"` | tsgo emits `.d.ts` and reports errors | **no tsgo, and no `.d.ts`** — an opt-out of types, right for terminal targets (app entries, dev servers, bundle inputs) whose declarations nothing consumes |
| `declarations = "oxc"` | Oxc emits `.d.ts`; tsgo validates | Oxc emits `.d.ts`; **nothing type-checks**. Oxc still enforces isolated declarations, so the declarations stay complete — what you give up is checking the function bodies |

`declarations = "oxc"` with `enable_check = False` is the only configuration
that runs no tsgo at all. If you use it broadly, keep a type-checking gate
somewhere — otherwise nothing in the build ever reads a type error.

## Cost of each mode

One measurement, one machine, one day. Reproducible from this tree — the script
is committed — but not a promise about your graph:

```bash
tools/bench_declarations.sh 20 50 3
```

1,000 annotated files across 20 packages in a single linear dependency chain
(so the critical path has somewhere to show up), medians of three interleaved
runs:

| Mode | Rebuild wall | Critical path |
|------|--------------|---------------|
| `declarations = "tsgo"` | 6.3s | 4.89s |
| `declarations = "oxc"` | 3.8s | 2.15s |
| `declarations = "oxc"`, `enable_check = False` | 2.7s | 1.06s |

Both `"tsgo"` and `"oxc"` run tsgo once per target, so the gap is not extra
work — it is serialisation. Under `"oxc"` the check is a validation action that
nothing waits for, so the critical path is Oxc's per-file transform; under
`"tsgo"` each of the 20 links waits for its dependency's declarations. Expect
the gap to shrink on shallower graphs and widen on deeper ones.

## Providers

- **`JsInfo`** — transitive depset of `.js` files, used by `ts_binary` and `ts_bundle`
- **`TsDeclarationInfo`** — depset of `.d.ts` files, used by downstream `ts_compile` targets for type resolution
- **`TsModuleInfo`** — the `module_name` this target is importable as, plus the
  directories its declarations land in, propagated transitively so a dependent
  can build its own `paths` entries
- **`OutputGroupInfo(_validation=...)`** — the tsgo check stamp. Only populated
  under `declarations = "oxc"`; under the default there is nothing to validate
  separately because the declarations themselves are the proof.
- **`OutputGroupInfo(strict_deps=...)`** — the `TsStrictDeps` stamp, on any
  target with `deps`. The compile actions take it as an input, so a violation
  fails a plain `bazel build`; the output group is there for reading the stamp
  and the checker in isolation.

## Architecture

The oxc-bazel binary processes each `.ts` file through:

1. Parse (oxc_parser)
2. Semantic analysis (oxc_semantic)
3. Isolated declarations emit (oxc_isolated_declarations) — only under `declarations = "oxc"`
4. TypeScript/JSX transform (oxc_transformer)
5. Code generation (oxc_codegen) for `.js` + `.js.map`

`TsStrictDeps` runs before the transform, as a Node action over a params-file
manifest of the target's declared and reachable providers. Its scanner is a
character walk over the source — a quoted string is a specifier only when the
tokens before it say so — which is the same walk Gazelle uses to generate deps,
so the two cannot demand different things.

tsgo runs as a separate Bazel action against the generated `tsconfig.json`.
Under `declarations = "tsgo"` that tsconfig sets `declaration`,
`emitDeclarationOnly`, `rootDir` and `outDir` so the emitted declarations land
beside Oxc's `.js` (mnemonic `TsgoDeclare`). Under `declarations = "oxc"` it
runs with `--noEmit` and writes only a stamp (mnemonic `TsgoCheck`).

## Note on output paths

Output paths are derived from source file names, not target names, so
`import "./foo"` resolves to `bazel-bin/.../foo.js`. A consequence: two
`ts_compile` targets in the same package cannot list the same source file —
Bazel reports conflicting actions. Split by directory, or give each target its
own sources.
