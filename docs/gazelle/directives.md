# Gazelle Directives Reference

Directives go in `BUILD.bazel` files as comments and control how Gazelle generates BUILD rules for that directory and its children.

## Full Reference

| Directive | Effect |
|-----------|--------|
| `# gazelle:ts_isolated_declarations false` | Emit `isolated_declarations = False` on all generated `ts_compile` and `ts_test` rules — the escape hatch for existing codebases |
| `# gazelle:ts_isolated_declarations true` | Re-enable isolated declarations for a subdirectory after a parent set it to `false` |
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

## Examples

### Existing codebase without explicit return types

```python
# BUILD.bazel (repo root)
load("@gazelle//:def.bzl", "gazelle")

# gazelle:ts_isolated_declarations false

gazelle(
    name = "gazelle",
    gazelle = "@rules_typescript//gazelle:gazelle_ts",
)
```

### Re-enable isolated declarations for a specific package

```python
# src/my-package/BUILD.bazel

# gazelle:ts_isolated_declarations true

# Gazelle will regenerate with isolated_declarations = True
```

### Index-only package boundaries (pre-0.2.0 behaviour)

```python
# BUILD.bazel (repo root)

# gazelle:ts_package_boundary index-only
```

### Path alias for `@/` imports

```python
# BUILD.bazel (repo root)

# gazelle:ts_path_alias @/ src/
```

This maps `import { x } from "@/utils"` to `//src/utils`.

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

### Exclude generated files

```python
# src/graphql/BUILD.bazel

# gazelle:ts_exclude *.generated.ts
```

Files matching `*.generated.ts` are excluded from `srcs` lists in this directory.
