# npm Dependencies

npm packages are managed through a `pnpm-lock.yaml` file. The `npm_translate_lock` extension downloads all packages and generates a self-contained `@npm` Bazel repository.

## Setup

**Step 1.** Create a `pnpm-lock.yaml` (use `pnpm install` in your project root).

**Step 2.** Add to `MODULE.bazel`:

```python
npm = use_extension("@rules_typescript//npm:extensions.bzl", "npm")
npm.translate_lock(pnpm_lock = "//:pnpm-lock.yaml")
use_repo(npm, "npm")
```

**Step 3.** Reference packages in BUILD files:

```python
ts_compile(
    name = "app",
    srcs = ["app.ts"],
    deps = ["@npm//:zod", "@npm//:react"],
)
```

## Label Convention

npm packages map to Bazel labels using this convention:

| npm package | Bazel label |
|-------------|-------------|
| `react` | `@npm//:react` |
| `react-dom` | `@npm//:react-dom` |
| `@types/react` | `@npm//:types_react` |
| `@tanstack/react-query` | `@npm//:tanstack_react-query` |

The general rules:
- Scoped packages (`@scope/name`) become `scope_name` (drop `@`, replace `/` with `_`)
- Hyphenated names are preserved as-is

## Platform-Specific Packages

The `npm_translate_lock` extension filters out packages whose `os`/`cpu` fields don't match the host machine. This handles packages like `@rollup/rollup-linux-x64-gnu` correctly without manual configuration.

## Bin Scripts

npm packages that define `bin` entries in their `package.json` get a `_bin` label automatically:

| npm package | Binary label |
|-------------|-------------|
| `vitest` | `@npm//:vitest_bin` |
| `esbuild` | `@npm//:esbuild_bin` |
| `oxlint` | `@npm//:oxlint_bin` |

Use these labels as `executable` targets in Bazel rules or as `tools` in custom actions.

## node_modules Targets

For test and dev-server targets that need a `node_modules` directory on the file system, use the `node_modules` rule:

```python
load("@rules_typescript//npm:defs.bzl", "node_modules")

node_modules(
    name = "node_modules",
    deps = ["@npm//:vitest", "@npm//:react"],
)
```

This creates a hermetic `node_modules` directory in the Bazel sandbox containing exactly the specified packages and their transitive dependencies.
