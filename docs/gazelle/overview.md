# Gazelle Overview

Gazelle auto-generates BUILD files from TypeScript source files, inferring `ts_compile` targets and resolving imports to Bazel labels.

## Setup

Add the Gazelle binary to your root `BUILD.bazel`:

```python
load("@gazelle//:def.bzl", "gazelle")

gazelle(
    name = "gazelle",
    gazelle = "@rules_typescript//gazelle:gazelle_ts",
)
```

Add `gazelle` to `MODULE.bazel`:

```python
bazel_dep(name = "gazelle", version = "0.47.0")
```

`rules_typescript` already declares `rules_go`, `go_sdk`, and `go_deps` as non-dev dependencies, so they propagate transitively via bzlmod. Consumers do not need to configure a Go toolchain.

Run Gazelle:

```bash
bazel run //:gazelle
```

## Package Boundary Heuristic

By default (**every-dir mode**), every directory that contains `.ts` or `.tsx` source files gets a `ts_compile` target. This matches Go's behaviour where every directory with `.go` files is a package.

**every-dir mode** (default): a directory becomes a boundary when it has any `.ts` files.

**index-only mode** (`# gazelle:ts_package_boundary index-only`): a directory becomes a boundary when:

1. It contains an `index.ts` or `index.tsx` file, or
2. It has the `# gazelle:ts_package_boundary true` directive, or
3. It is the repository root.

!!! note "Upgrading from pre-0.2.0"
    Earlier versions used **index-only mode** by default. If you relied on that behaviour, add `# gazelle:ts_package_boundary index-only` to your root `BUILD.bazel` to restore it.

Test files (`*.test.ts`, `*.spec.ts`, `*.test.tsx`, `*.spec.tsx`) generate `ts_test` targets automatically in both modes.

## Automatic Lint Targets

When a linter config file is present in the current directory or any ancestor, Gazelle automatically generates a `ts_lint` target alongside each `ts_compile` target. The lint target name is the compile target name with `_lint` appended.

Detected config files:
- **oxlint**: `oxlint.json`, `.oxlintrc.json`, `.oxlintrc`
- **eslint**: `eslint.config.mjs`, `eslint.config.js`, `.eslintrc.json`, `.eslintrc.*`

oxlint configs are detected before ESLint configs. The closest config file wins.

Example generated output with an `oxlint.json` at the repo root:

```python
ts_compile(
    name = "my_lib",
    srcs = ["index.ts"],
    visibility = ["//visibility:public"],
)

ts_lint(
    name = "my_lib_lint",
    srcs = ["index.ts"],
    linter = "oxlint",
    linter_binary = "@npm//:oxlint_bin",
    config = "//:oxlint.json",
)
```

To run linting:

```bash
bazel build //... --output_groups=+_validation
```

## gazelle_ts.json

Place a `gazelle_ts.json` file in your repository root (or any subtree root) to configure path aliases, npm package mappings, and runtime dependencies:

```json
{
  "pathAliases": {
    "@/": "src/",
    "@components/": "src/components/"
  },
  "npmMappingFile": "npm/package_mapping.json",
  "excludePatterns": ["*.generated.ts"],
  "excludeDirs": ["coverage", "storybook-static"],
  "runtimeDeps": {
    "test": ["@npm//:happy-dom", "@npm//:react", "@npm//:react-dom"]
  }
}
```

`pathAliases` maps TypeScript `paths` compilerOptions to workspace-relative directories, so imports like `import { Button } from "@components/Button"` resolve to `//src/components`.

`runtimeDeps.test` lists Bazel labels appended to every generated `ts_test` deps list. Use this for packages needed at test runtime but never statically imported:

| Package | Why it needs to be explicit |
|---------|----------------------------|
| `@npm//:happy-dom` | vitest environment — imported by vitest config, not your test files |
| `@npm//:react` | JSX runtime (`react/jsx-runtime`) — never directly imported |
| `@npm//:react-dom` | required for React test utilities |
| `@npm//:types_react` | type declarations for JSX |

## Import Resolution

Gazelle resolves TypeScript imports to Bazel labels in this order:

1. **Relative imports** (`./foo`, `../bar`) — resolved to the `ts_compile` target in that directory
2. **Path aliases** — resolved via `pathAliases` in `gazelle_ts.json` or `# gazelle:ts_path_alias` directives
3. **npm packages** — resolved to `@npm//:<label>` using the pnpm lockfile
4. **Unresolved** — optionally warned with `# gazelle:ts_warn_unresolved true`

See [Directives Reference](directives.md) for all available directives.
