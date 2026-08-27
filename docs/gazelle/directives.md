# Gazelle Directives Reference

Directives go in `BUILD.bazel` files as comments and control how Gazelle generates BUILD rules for that directory and its children.

## Full Reference

| Directive | Effect |
|-----------|--------|
| `# gazelle:ts_declarations oxc` | Emit `declarations = "oxc"` on all generated `ts_compile` and `ts_test` rules in this tree — Oxc emits `.d.ts` syntactically, which requires an explicit type on every export |
| `# gazelle:ts_declarations tsgo` | Return a subdirectory to the default emitter after a parent set `oxc` |
| `# gazelle:ts_package_boundary every-dir` | (default) Every directory with `.ts` files becomes a package |
| `# gazelle:ts_package_boundary index-only` | Only directories with `index.ts`/`.tsx` become packages (pre-0.2.0 behaviour) |
| `# gazelle:ts_package_boundary true` | Mark this single directory as a boundary (useful in index-only mode without `index.ts`) |
| `# gazelle:ts_ignore` | Suppress TypeScript rule generation for this directory and its children |
| `# gazelle:ts_ignore false` | Re-enable generation after a parent used `ts_ignore` |
| `# gazelle:ts_target_name my_lib` | Override the default target name (which is the directory basename) |
| `# gazelle:ts_path_alias @/ src/` | Map a TypeScript path alias to a workspace-relative directory |
| `# gazelle:ts_runtime_dep @npm//:happy-dom` | Append a label to every generated `ts_test` deps list |
| `# gazelle:ts_exclude *.generated.ts` | Exclude files matching this pattern from source targets |
| `# gazelle:ts_warn_unresolved true` | Warn when an import cannot be resolved to a Bazel label |
| `# gazelle:ts_codegen <name> <generator> <outs> [args…]` | Register a `ts_codegen` target in this directory |
| `# gazelle:ts_npm_hub npm_eslint` | Resolve bare specifiers in this tree into that npm hub repo instead of `@npm` |

That is the complete set — ten directives. An unknown `# gazelle:ts_*` comment
makes Gazelle warn rather than fail, so a typo shows up in the run output.

## `# keep` — not ours, and load-bearing

`# keep` is Gazelle's own comment, understood by every language extension. On the
line above an attribute it means "never rewrite this value"; above a whole rule it
means "never rewrite this rule". It is the answer whenever a run keeps undoing an
edit you meant:

```python
ts_compile(
    name = "internal",
    srcs = ["index.ts"],
    # keep
    visibility = ["//myapp:__subpackages__"],
)
```

`visibility` is the case that bites, because it is a *merged* attribute and every
rule Gazelle generates carries `//visibility:public` — so without `# keep` a
hand-narrowed visibility widens back on every run. See
[getting the clean-tree diff to empty](overview.md#getting-the-clean-tree-diff-to-empty).

### Attributes Gazelle owns

Gazelle recomputes these from the tree on every run, so a value it cannot derive
is **replaced** unless a `# keep` holds it. The set is not framework-specific —
`ts_compile.deps` is as much Gazelle's as `next_build.staging_srcs` is:

| Rule | Attributes Gazelle owns |
|------|-------------------------|
| `ts_compile` | `srcs`, `deps`, `visibility`, `path_aliases`, `declarations` |
| `ts_test` | `srcs`, `deps` |
| `ts_lint` | `srcs`, `linter`, `linter_binary`, `config`, `fail_on_warnings` |
| `css_library`, `css_module`, `asset_library`, `json_library` | `srcs`, `deps`, `visibility` |
| `ts_codegen` | `outs`, `out_dir`, `visibility` |
| `next_build` | `srcs` (a `glob()`), `staging_srcs`, `config`, `tsconfig`, `node_modules` |
| `next_dev_server` | `node_modules` |
| `sveltekit_build` | `srcs` (a `glob()`), `staging_srcs`, `config`, `svelte_config`, `node_modules` |
| `ts_bundle` (framework root) | `staging_srcs`, `entry_point`, `html`, `vite_config`, `mode`, `bundler` |
| `vite_bundler` | `vite`, `node_modules` |
| `node_modules` (framework root) | `deps` |
| `filegroup(name = "sources")` | `srcs`, `visibility` |

`ts_dev_server` is the exception: it is written once, when no rule of that name
exists, and left alone from then on. Its attributes are yours after the first
run, `# keep` or not.

`# keep` works at three granularities, and all three are honoured on both write
paths — the merger's, and the direct write a `glob()` needs because the merger
cannot merge a call expression:

```python
next_build(
    name = "app",
    # A single pattern Gazelle did not derive: an assets tree the framework
    # config points somewhere unconventional.
    srcs = glob([
        "app/**",
        "content/**",  # keep
    ]),
    # A dep no import implies: next/image loads sharp at runtime.
    node_modules = ":node_modules",
    staging_srcs = [
        "//lib",
        "//vendor:vendor_hand",  # keep
    ],
    # keep
    config = "custom.next.config.mjs",
)
```

A run that drops a value from one of these attributes names it:

```
typescript: next_build(app) in BUILD.bazel: Gazelle generates staging_srcs and
recomputed it from the tree, so "//vendor:vendor_hand" is no longer declared. A
value Gazelle cannot derive needs a "# keep" comment on its own line to survive
the next run; "# keep" above the attribute hands the whole attribute back to you.
```

That line is the contract's other half. A declared build input disappearing in
silence is the failure this extension exists to remove, and a Gazelle run
deleting one is the same failure inverted — so every dropped value is named,
whether it was a stale label Gazelle itself wrote or an edit of yours. Naming
them is an addition: Gazelle's Go extension drops the same values silently, and
what survives a run is identical either way.

One value is deliberately not named: one whose file or package is no longer on
disk. Deleting a staged directory drops the label that named it, and telling you
to hold that label with `# keep` would have you name a source nothing provides —
which fails analysis instead of surviving the run. An ordinary deletion is
therefore silent, and a value whose target is still there is not.

#### Values Gazelle cannot merge

`# keep` decides what survives among plain strings and plain lists of plain
strings — the two shapes Gazelle's merger reconciles value by value. A value in
any other shape it cannot merge at all: a module-level variable, two lists
joined with `+`, a `select()`, or a list with one variable element.

**What happens then is Gazelle's decision, not this extension's**, and it goes
two ways depending on the shape: a bare variable is replaced with the value
Gazelle derived, and whatever the variable held is gone; two lists joined with
`+` are refused, and the attribute keeps your expression while Gazelle stops
recomputing it. This is the same behaviour `rules_go`'s extension has, for the
same reason — the decision lives in Gazelle's merger, which both extensions
call. What this extension adds is that neither outcome is silent:

```
typescript: BUILD.bazel:28: next_build(app) declares staging_srcs as an
expression Gazelle's merger cannot reconcile value by value, so staging_srcs is
no longer an attribute Gazelle maintains: it either replaces the whole
expression, losing what it computed, or leaves it untouched and stops updating
it. A "# keep" comment above the attribute makes that yours deliberately.
```

Either way the attribute has stopped being maintained, so treat that line as a
decision to make rather than a warning to live with. Two ways to resolve it:
put `# keep` above the attribute and own it, or rewrite the value as a plain
list of strings with `# keep` on the entries Gazelle cannot derive, which hands
the attribute back to it.

A `glob()` is the one shape this extension decides itself, because the merger
never sees it: `srcs` on `next_build` and `sveltekit_build` is written directly.
A `glob()` whose arguments are not plain lists of strings is left alone —
`rule.ParseGlobExpr` reads only part of such a call, so rewriting from what it
read would drop the rest — and the run says so in the merger's own words, the
file and the span of the value:

```
typescript: BUILD.bazel:28.20-28.25: could not merge expression -- next_build(app)
declares srcs that is not a glob() of plain strings, so Gazelle left it alone.
```

## Examples

### Existing codebase without explicit return types

Nothing to configure. The `ts_compile` default (`declarations = "tsgo"`) emits
declarations from the full type program, so inferred export types are fine:

```python
# BUILD.bazel (repo root)
load("@gazelle//:def.bzl", "gazelle")

gazelle(
    name = "gazelle",
    gazelle = "@rules_typescript//gazelle:gazelle_ts",
)
```

### Opt one package into Oxc declaration emit

Once every export in a package carries an explicit type, move it to Oxc's
syntactic emit to take type-checking off the critical path
(see [Isolated Declarations](../getting-started/isolated-declarations.md)):

```python
# src/my-package/BUILD.bazel

# gazelle:ts_declarations oxc

# Gazelle regenerates with declarations = "oxc". Oxc fails the build, naming
# the file and line, for any export it cannot derive a type from.
```

An unrecognised value keeps the inherited emitter and logs a warning, so a typo
cannot silently demand annotations across a whole tree.

### Index-only package boundaries (pre-0.2.0 behaviour)

```python
# BUILD.bazel (repo root)

# gazelle:ts_package_boundary index-only
```

In this mode a directory without an index file is not a package, so its sources
are rolled up into the nearest ancestor that is one, and no BUILD file is written
there. That is what dissolves the commonest package-level cycle: a barrel that
re-exports `./rules` while `./rules` imports `../utils` is a cycle between two
Bazel packages and no cycle at all between files.

### More than one npm hub

A workspace can translate several lockfiles. Which hub a package's imports come
from is a property of that package, so it is named where the package is:

```python
# eslint-plugin/BUILD.bazel

# gazelle:ts_npm_hub npm_eslint
```

Generated deps in that tree then read `@npm_eslint//:eslint` rather than
`@npm//:eslint`. Without it the label names a hub the package does not use, which
is a label that does not exist. Both `npm_eslint` and `@npm_eslint` are accepted.

The directive names a hub; declaring one is `npm.translate_lock(name = ...)` plus
a matching `use_repo`, and each hub also needs its own `ts_add_package` target —
see [More than one hub](../guides/npm.md#more-than-one-hub).

### Path alias for `@/` imports

```python
# BUILD.bazel (repo root)

# gazelle:ts_path_alias @/ src/
```

This maps `import { x } from "@/utils"` to `//src/utils`.

A generated target carries the aliases its own imports match, plus any alias
whose directory holds its own sources — which is exactly the set `ts_compile`
accepts, so a generated target cannot trip
[its alias validation](../rules/ts-compile.md#the-two-hard-errors). The practical
effect is that the directive reaches `compilerOptions.paths` in the
[IDE tsconfig](../getting-started/ide-setup.md) as soon as you declare it,
without waiting for a source to import through it. Aliases Gazelle reads back out
of a `tsconfig.json` this ruleset generated are an echo of its own output, not a
declaration, and are not re-emitted.

### Add runtime deps to all tests

```python
# BUILD.bazel (repo root)

# gazelle:ts_runtime_dep @npm//:happy-dom
# gazelle:ts_runtime_dep @npm//:react
```

These labels are appended to every generated `ts_test` deps list in the repo.

### Suppress generation for a directory

```python
# legacy-code/BUILD.bazel

# gazelle:ts_ignore
```

Gazelle will not generate `ts_compile` or `ts_test` targets in `legacy-code/` or any of its subdirectories. Write the BUILD file for this directory manually.

### Register a codegen target

```python
# src/api/BUILD.bazel

# gazelle:ts_codegen api_types @npm//:openapi-typescript_bin api-types.ts {srcs} -o {out}
```

The fields are `<name> <generator_label> <outs> [args…]`, where `<outs>` is a
comma-separated list of output file names and everything after it is passed to
the generator. `{srcs}` and `{out}` are substituted.

For a generator that writes a whole directory, prefix the outs field with
`dir:`:

```python
# gazelle:ts_codegen prisma_client @npm//:prisma_bin dir:generated/client generate --schema {srcs}
```

Gazelle also auto-detects Prisma, GraphQL codegen and OpenAPI generators from
`package.json`, so a directive is only needed for generators it does not
recognise. TanStack Router is deliberately not among them: its route tree is
written by the Start Vite plugin during the bundle, into the writable staging
directory `ts_bundle` hands it, so a second copy in `bazel-bin` only drifts from
the one the build actually used.

### Exclude generated files

```python
# src/graphql/BUILD.bazel

# gazelle:ts_exclude *.generated.ts
```

Files matching `*.generated.ts` are excluded from `srcs` lists in this directory.
