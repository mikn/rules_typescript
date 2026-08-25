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

### What a run is checked against

Taking Gazelle's output wholesale is the intended workflow. Two of the four
properties that makes reasonable are pinned in CI, by
`//tests/integration:gazelle_roundtrip_test`, which runs against a real nested
workspace: **the output builds**, and **generating it twice from scratch produces
byte-identical BUILD files**. The other two — the test suite still passing, and
the set of test targets being unchanged — were verified against this repository's
own tree during development but are not a CI job.

The fourth is the one worth running yourself on an existing repository, because
the first three do not imply it: a Gazelle run that *deletes* a test satisfies
"builds" and "idempotent" both.

```bash
bazel query 'tests(//...)' | sort > before
bazel run //:gazelle
bazel query 'tests(//...)' | sort | diff before -
```

That has caught a real deletion, and the mechanism was not the TypeScript
extension: Gazelle's **Go** language turns `# gazelle:exclude *_test.go` into a
deletion stub named `<dirbase>_test`, so a hand-written `go_test` with that exact
name disappears. Worth knowing if your repository is polyglot.

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

## Framework detection

When the workspace-root `package.json` names a framework Gazelle recognises, the
root BUILD file gets that framework's bundle wiring — a `node_modules` tree, a
`vite_bundler`, and a `ts_bundle` with `staging_srcs`, `vite_config` and
`entry_point` already set. Detection is by dependency name, so there is nothing
to configure.

Recognising a framework and being able to bundle it are two different things,
and the table says which is which:

| `package.json` names | Gazelle emits |
|---|---|
| `@tanstack/react-router`, `@tanstack/start` | the Vite bundle targets |
| `@remix-run/dev`, `@remix-run/react` | the Vite bundle targets |
| `next` | `node_modules` + `next_build` — its own rule, not Vite |
| `@sveltejs/kit` | nothing, plus a message saying why |
| `@solidjs/start`, `solid-start` | nothing, plus a message saying why |

For the last two, no BUILD file a user could write closes the gap, so emitting a
`ts_bundle` would only produce a target that fails `bazel build //...` — and a
target silently missing is worse still. Gazelle logs the framework, the reason,
and the fallback:

```
typescript: SolidStart detected: bundling it is unsupported, so no bundle target
was generated — @solidjs/start ships no Vite plugin: defineConfig() returns a
vinxi app, which ts_bundle's vite_config contract (a default export with a
plugins array) cannot consume. Your TypeScript still compiles and tests; for a
client-only build, declare a ts_bundle by hand with no vite_config.
```

SvelteKit's reason is different in kind: its plugin runs SvelteKit's own sync
step from the Vite `config` hook, which wants a `src/app.html` and a
`svelte.config.js` of its own beside the Vite config — and `.svelte` files are
not TypeScript, so no `staging_srcs` filegroup Gazelle emits carries the routes.

### The entry point is yours to declare

`ts_bundle` takes exactly one `.js` as its entry, and Gazelle merges every source
in a directory into one target — so the framework's conventional client entry has
to be its own single-file target, which is what the generated `entry_point` label
names. Mark that file excluded and declare the target:

```python
# app/BUILD.bazel
# gazelle:ts_exclude entry.client.tsx

load("@rules_typescript//ts:defs.bzl", "ts_compile")

ts_compile(
    name = "entry_client",
    srcs = ["entry.client.tsx"],
    visibility = ["//visibility:public"],
)
```

Until that target exists the generated `entry_point` names nothing, and
`bazel build //...` fails on the dangling label — for the whole workspace, not
just that one target. `//tests/integration:remix_test` pins both halves: green
with the target present, and failing on exactly that label without it.

## Import Resolution

Gazelle resolves TypeScript imports to Bazel labels in this order:

1. **Relative imports** (`./foo`, `../bar`) — resolved to the `ts_compile` target in that directory
2. **Path aliases** — from `compilerOptions.paths` in the nearest `tsconfig.json`, or a `# gazelle:ts_path_alias` directive
3. **A first-party `module_name`** — a bare specifier is matched against the `module_name` of the indexed `ts_compile` targets *before* npm is considered, because the `@npm` hub has no package under that name
4. **npm packages** — resolved to `@npm//:<label>` using the pnpm lockfile
5. **Unresolved** — optionally warned with `# gazelle:ts_warn_unresolved true`

A specifier that spells out an extension resolves the same as one that does not.
`./rules/foo.js`, `./rules/foo.ts` and `./rules/foo` are matched against one
candidate list — the path as written, the path with its extension dropped, that
stem under each known extension, and `<stem>/index.ts[x]` — which is what makes
NodeNext-style `.js` specifiers over `.ts` sources resolve to the target that
owns the source.

Node built-ins get no dep, with or without the `node:` prefix: `import "path"`
and `import "node:path"` are both left alone, matching what the strict-deps check
exempts.

When several alias entries match one specifier — a tsconfig declaring both
`"@shared"` and `"@shared/*"`, which is ordinary — the **longest matching alias
key wins**. That is TypeScript's own rule: a pattern equal to the whole
specifier is necessarily the longest key that can match it, so "exact beats
wildcard" and "most specific wildcard wins" are the same rule. An alias key
without a trailing wildcard matches only at a path-segment boundary, so
`@shared` does not claim `@sharedX`.

Gazelle's deps and the `ts_compile` strict-deps check share one specifier
scanner, so a failing build is one Gazelle can fix. If `bazel build` reports an
import no direct dep provides and re-running Gazelle does not add it, that is a
bug in the ruleset rather than something to work around.

See [Directives Reference](directives.md) for all available directives.
