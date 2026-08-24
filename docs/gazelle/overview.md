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

`rules_typescript` declares `rules_go`, `go_sdk` and `go_deps` as non-dev
dependencies, so a consumer needs only the `bazel_dep` above: the Go toolchain
and the Gazelle binary's own Go module deps come along transitively. That is a
convenience for you and a real cost in the dependency graph — building the
Gazelle extension fetches a Go SDK and its modules, on top of the Rust toolchain
`oxc-bazel` needs.

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

## Generated Target Names

| Rule | Name |
|------|------|
| `ts_compile` | directory basename (`src/components` → `components`), or `# gazelle:ts_target_name` |
| `ts_test` | `<ts_compile name>_test` |
| `ts_lint` | `<ts_compile name>_lint` |
| `ts_dev_server` | `dev` |
| `css_library`, `css_module`, `asset_library`, `json_library` | the source filename with `.` replaced by `_` |

Non-TypeScript libraries keep the extension in the name — `button.css` →
`button_css`, `logo.svg` → `logo_svg`, `config.json` → `config_json`,
`Button.module.css` → `Button_module_css`. That keeps the directory-named
`ts_compile` target free (a `components/` directory holding `components.css`
would otherwise generate two targets named `components`) and keeps files that
share a stem apart (`logo.svg` and `logo.json`). If two names still tie, the
later one gets a numeric suffix (`_2`).

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

## Configuration

Gazelle reads `compilerOptions.paths` and `compilerOptions.baseUrl` straight
from the nearest `tsconfig.json`, parsed as JSONC — comments and trailing commas
are fine. Everything else is configured with `# gazelle:ts_*` directives in
BUILD files; see the [Directives Reference](directives.md).

Directives beat file-based configuration, and the aliases from a `tsconfig.json`
merge with the ones a parent directory's directives established.

### gazelle_ts.json (deprecated)

A `gazelle_ts.json` in a directory is still read, and Gazelle prints a
deprecation warning naming the directive to replace each key with:

| Key | Replacement |
|---|---|
| `pathAliases` | `# gazelle:ts_path_alias @/ src/` |
| `excludePatterns` | `# gazelle:ts_exclude *.generated.ts` |
| `runtimeDeps.test` | `# gazelle:ts_runtime_dep @npm//:happy-dom` |
| `excludeDirs` | no directive; excluded directories are the built-in set plus this key |
| `npmMappingFile` | no directive; a JSON file mapping npm names to labels |

It sits above `tsconfig.json` and below directives in precedence. Do not add new
uses of it — a config file that is invisible from the BUILD files it changes was
a mistake, and only the two keys with no directive replacement keep it alive.

`runtimeDeps.test` (or `# gazelle:ts_runtime_dep`) lists Bazel labels appended to every generated `ts_test` deps list. Use this for packages needed at test runtime but never statically imported:

| Package | Why it needs to be explicit |
|---------|----------------------------|
| `@npm//:happy-dom` | vitest environment — imported by vitest config, not your test files |
| `@npm//:react` | JSX runtime (`react/jsx-runtime`) — never directly imported |
| `@npm//:react-dom` | required for React test utilities |
| `@npm//:types_react` | type declarations for JSX |

## Import Resolution

Gazelle resolves TypeScript imports to Bazel labels in this order:

1. **Relative imports** (`./foo`, `../bar`) — resolved to the `ts_compile` target in that directory
2. **Path aliases** — from `compilerOptions.paths` in the nearest `tsconfig.json`, or a `# gazelle:ts_path_alias` directive
3. **npm packages** — resolved to `@npm//:<label>` using the pnpm lockfile
4. **Unresolved** — optionally warned with `# gazelle:ts_warn_unresolved true`

See [Directives Reference](directives.md) for all available directives.
