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
| `srcs` | `label_list` | required | `.ts`, `.tsx`, or `.d.ts` files |
| `deps` | `label_list` | `[]` | `ts_compile` or `ts_npm_package` targets |
| `target` | `string` | `"es2022"` | ECMAScript target version |
| `jsx_mode` | `string` | `"react-jsx"` | JSX transform: `react-jsx`, `react`, `preserve` |
| `declarations` | `string` | `"tsgo"` | Which tool emits `.d.ts`: `"tsgo"` or `"oxc"` |
| `enable_check` | `bool` | `True` | Run tsgo. See [Turning tsgo off](#turning-tsgo-off) |
| `path_aliases` | `string_dict` | `{}` | Alias prefix → workspace-relative directory |
| `vite_types` | `bool` | `False` | Prepend the Vite ambient type shim to `srcs` |

## Outputs

For each source file `foo.ts`:

| Output | Description |
|--------|-------------|
| `foo.js` | Compiled JavaScript (always from Oxc) |
| `foo.js.map` | Source map |
| `foo.d.ts` | Declaration file — the compilation boundary |

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

Measured with `tools/bench_declarations.sh` on a generated corpus of 1,000
annotated files across 20 packages in one linear dependency chain (so the
critical path has somewhere to show up). Medians of three interleaved runs:

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

Re-run the numbers on your own graph:

```bash
tools/bench_declarations.sh [PACKAGES] [FILES_PER_PACKAGE] [ITERATIONS]
```

## Providers

- **`JsInfo`** — transitive depset of `.js` files, used by `ts_binary` and `ts_bundle`
- **`TsDeclarationInfo`** — depset of `.d.ts` files, used by downstream `ts_compile` targets for type resolution
- **`OutputGroupInfo(_validation=...)`** — the tsgo check stamp. Only populated
  under `declarations = "oxc"`; under the default there is nothing to validate
  separately because the declarations themselves are the proof.

## Architecture

The oxc-bazel binary processes each `.ts` file through:

1. Parse (oxc_parser)
2. Semantic analysis (oxc_semantic)
3. Isolated declarations emit (oxc_isolated_declarations) — only under `declarations = "oxc"`
4. TypeScript/JSX transform (oxc_transformer)
5. Code generation (oxc_codegen) for `.js` + `.js.map`

tsgo runs as a separate Bazel action against a generated `tsconfig.json` that
uses `rootDirs` to bridge the source tree and the output tree, plus
`moduleResolution: "Bundler"`, `paths` entries for npm packages and
`typeRoots` for `@types/*`. Under `declarations = "tsgo"` that tsconfig sets
`declaration`, `emitDeclarationOnly`, `rootDir` and `outDir` so the emitted
declarations land beside Oxc's `.js` (mnemonic `TsgoDeclare`). Under
`declarations = "oxc"` it runs with `--noEmit` and writes only a stamp
(mnemonic `TsgoCheck`).

## Note on output paths

Output paths are derived from source file names, not target names, so
`import "./foo"` resolves to `bazel-bin/.../foo.js`. A consequence: two
`ts_compile` targets in the same package cannot list the same source file —
Bazel reports conflicting actions. Split by directory, or give each target its
own sources.
