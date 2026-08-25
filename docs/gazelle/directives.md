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
hand-narrowed visibility widens back on every run, silently. See
[getting the clean-tree diff to empty](overview.md#getting-the-clean-tree-diff-to-empty).

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

Gazelle also auto-detects TanStack Router, Prisma, GraphQL codegen and OpenAPI
generators from `package.json`, so a directive is only needed for generators it
does not recognise.

### Exclude generated files

```python
# src/graphql/BUILD.bazel

# gazelle:ts_exclude *.generated.ts
```

Files matching `*.generated.ts` are excluded from `srcs` lists in this directory.
