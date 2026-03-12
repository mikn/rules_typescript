# rules_typescript

Bazel rules for TypeScript using [Oxc](https://oxc.rs/) for compilation and [tsgo](https://github.com/nicholasgasior/TypeScript-7) for type-checking.

**TypeScript on Bazel should feel like Go on Bazel.** Write `.ts` files, let Gazelle generate BUILD files, get hermetic cached builds with sub-second incremental feedback.

## Key Ideas

- **Oxc compiles** — A Rust-based TypeScript/JSX transformer produces `.js`, `.js.map`, and `.d.ts` per source file. No `tsc`. Hundreds of files in milliseconds.
- **tsgo type-checks** — The Go port of TypeScript runs as a Bazel validation action. Type errors surface on `bazel build` but don't block downstream compilation.
- **Isolated declarations** — By enforcing explicit return types on exports, `.d.ts` emit is a per-file syntactic transform. This is the architectural keystone: changing an implementation without changing the public API doesn't recompile dependents.

## Requirements

The only prerequisite is **Bazelisk** (or Bazel 9+ directly). Everything else — the Rust toolchain, Go toolchain, Node.js runtime, and all npm packages — is fetched hermetically by Bazel on the first build.

Supported platforms:

- Linux x86_64
- Linux ARM64
- macOS x86_64
- macOS ARM64

### Install Bazelisk

Bazelisk is a launcher that reads `.bazelversion` and downloads the correct Bazel version automatically.

```bash
# macOS (Homebrew)
brew install bazelisk

# Linux / macOS (manual)
curl -Lo ~/.local/bin/bazel \
  https://github.com/bazelbuild/bazelisk/releases/latest/download/bazelisk-linux-amd64
chmod +x ~/.local/bin/bazel

# Windows (Scoop)
scoop install bazelisk
```

After installing, `bazel` on your PATH is Bazelisk. No further setup is needed.

---

## Quick Start

The first build fetches all toolchains (Rust compiler, Go compiler, Node.js runtime) — typically 2-5 minutes on first run. Subsequent builds are fully cached and take milliseconds for small changes.

Choose your path:

- [Path A: New project](#path-a-new-project) — starting from scratch
- [Path B: Existing project](#path-b-existing-project-migration) — migrating a TypeScript codebase

---

### Path A: New Project

**Step 1.** Create `.bazelversion`:

```
9.0.0
```

**Step 2.** Create `WORKSPACE.bazel` (empty file — required by Bazel 9):

```
```

**Step 3.** Create `MODULE.bazel`:

```python
module(
    name = "my_project",
    version = "0.0.0",
)

bazel_dep(name = "rules_typescript", version = "0.1.0")

register_toolchains("@rules_typescript//ts/toolchain:all")

bazel_dep(name = "gazelle", version = "0.47.0")
```

**Step 4.** Create `.bazelrc`:

```
build --incompatible_strict_action_env
build --nolegacy_external_runfiles
build --output_groups=+_validation
```

The `--output_groups=+_validation` line makes type errors fail `bazel build`, the same as `go build`.

**Step 5.** Create `BUILD.bazel` at the repo root:

```python
load("@gazelle//:def.bzl", "gazelle")

gazelle(
    name = "gazelle",
    gazelle = "@rules_typescript//gazelle:gazelle_ts",
)
```

**Step 6.** Write your TypeScript files. Use explicit return types on exported functions (explained in [Isolated Declarations](#isolated-declarations)):

```typescript
// src/lib/math.ts
export function add(a: number, b: number): number {
  return a + b;
}
```

**Step 7.** Generate BUILD files:

```bash
bazel run //:gazelle
```

**Step 8.** Build and type-check:

```bash
bazel build //...
```

**Step 9.** Run tests:

```bash
bazel test //...
```

That's it. Each `ts_compile` target Gazelle generates produces `.js`, `.js.map`, and `.d.ts` outputs per source file.

---

### Path B: Existing Project (Migration)

**Step 1.** Set up the same four root files as Path A (`.bazelversion`, `WORKSPACE.bazel`, `MODULE.bazel`, `.bazelrc`).

**Step 2.** Create `BUILD.bazel` at the repo root with one extra directive:

```python
load("@gazelle//:def.bzl", "gazelle")

# gazelle:ts_isolated_declarations false

gazelle(
    name = "gazelle",
    gazelle = "@rules_typescript//gazelle:gazelle_ts",
)
```

The `# gazelle:ts_isolated_declarations false` directive is the escape hatch for existing codebases. Without it, Gazelle generates `ts_compile` targets with `isolated_declarations = True`, which requires every exported function and variable to have an explicit return type — a constraint most existing TypeScript projects don't satisfy. With the directive, Gazelle sets `isolated_declarations = False` everywhere: your code compiles immediately, you get hermetic builds and caching, but not yet the maximum incremental speed (see [Isolated Declarations](#isolated-declarations) for why this matters).

**Step 3.** Run Gazelle:

```bash
bazel run //:gazelle
```

**Step 4.** Build everything:

```bash
bazel build //...
```

If there are type errors, fix them. The `isolated_declarations = False` flag means you won't hit "missing return type" errors yet.

**Step 5.** Migrate packages to isolated declarations one at a time. See [Isolated Declarations — Migration](#migration) for the step-by-step process.

---

### Version pinning

The `ts` extension lets you pin specific tool versions. Add to `MODULE.bazel`:

```python
# Pin tsgo to a specific release.  The root module's value wins.
ts = use_extension("@rules_typescript//ts:extensions.bzl", "ts")
ts.tsgo(version = "7.0.0-dev.20260311.1")
```

To pin Node.js:

```python
node = use_extension("@rules_nodejs//nodejs:extensions.bzl", "node")
node.toolchain(
    name = "nodejs",
    node_version = "22.14.0",
)
```

Your version takes precedence over the default bundled with `rules_typescript` because bzlmod resolves root-module extension calls first.

### IDE setup (VS Code / WebStorm)

Run once to generate a workspace-root `tsconfig.json` that your IDE uses for code intelligence:

```bash
bazel run //:refresh_tsconfig
```

Re-run whenever you add or remove packages. **VS Code**: run `TypeScript: Restart TS Server` from the command palette after regenerating.

---

## Isolated Declarations

This is the concept that makes everything fast. Read this before writing code.

### What it means

Normally TypeScript generates `.d.ts` declaration files by running full type inference across your project — it needs to know the return type of `add()` in `math.ts` before it can write `math.d.ts`. That means changing any `.ts` file can potentially invalidate `.d.ts` files across the project, forcing Bazel to recompile downstream packages.

With isolated declarations, each file's `.d.ts` is generated from that file alone, with no cross-file inference — because you wrote the return types explicitly. This is the architectural keystone: if you change `math.ts` without changing its exported types, its `.d.ts` is identical. Bazel sees no change at the dependency boundary and skips all downstream packages.

### The requirement

```typescript
// This fails with isolated_declarations = True (return type is inferred)
export function add(a: number, b: number) {
  return a + b;
}

// This works (explicit return type)
export function add(a: number, b: number): number {
  return a + b;
}
```

The rule applies to every exported function, arrow function, and variable. The tsgo type-checker reports missing annotations as:

```
error TS9007: Declaration emit for this file requires type resolution. ...
```

### What happens without it

Setting `isolated_declarations = False` (via the Gazelle directive or the rule attribute) tells oxc to fall back to emitting `.d.ts` with inferred types. The build still works, you still get hermetic caching, but the `.d.ts` boundary is less precise: a change that doesn't affect public types may still cause downstream recompilation because Bazel can't prove the `.d.ts` is unchanged.

### Migration

If you have an existing codebase, start with `# gazelle:ts_isolated_declarations false` (see [Path B](#path-b-existing-project-migration)) and migrate one package at a time:

**Step 1.** Install the ESLint plugin that reports missing annotations.

The plugin is not yet published to npm. Build it from the `eslint-plugin/` directory in the `rules_typescript` repository and install the resulting tarball:

```bash
# From the rules_typescript checkout:
cd path/to/rules_typescript/eslint-plugin
npm install
npm pack
# This produces rules_typescript-eslint-plugin-isolated-declarations-0.1.0.tgz

# In your project:
npm install --save-dev \
  path/to/rules_typescript/eslint-plugin/rules_typescript-eslint-plugin-isolated-declarations-0.1.0.tgz \
  @typescript-eslint/parser \
  eslint
```

Configure it in `eslint.config.js`:

```js
import isolatedDeclarations from '@rules_typescript/eslint-plugin-isolated-declarations';

export default [
  {
    plugins: { 'isolated-declarations': isolatedDeclarations },
    rules: { 'isolated-declarations/require-explicit-types': 'error' },
  },
];
```

**Step 2.** Pick one package. Run the linter on it:

```bash
npx eslint src/my-package/
```

**Step 3.** Fix the reported violations — add explicit return types and type annotations to all exported symbols.

**Step 4.** Add `# gazelle:ts_isolated_declarations true` to that package's `BUILD.bazel` (or remove the `false` directive if you set it per-directory). Re-run Gazelle to regenerate:

```bash
bazel run //:gazelle
bazel build //src/my-package --output_groups=+_validation
```

**Step 5.** Repeat for the next package. Each migrated package immediately benefits from the faster incremental boundary.

### What the ESLint rule covers

| Export pattern | Flagged when |
|----------------|-------------|
| `export function foo() {}` | No `: ReturnType` annotation |
| `export const fn = () => ...` | No return type on arrow or binding annotation |
| `export const x = someExpression` | No `: Type` annotation on binding |
| `export default function() {}` | No `: ReturnType` annotation |

The rule does NOT flag `export type`, `export interface`, `export class`, `export enum`, re-exports (`export { x } from '...'`), or ambient declarations.

---

## npm Dependency Management

npm packages are managed through a `pnpm-lock.yaml` file. The `npm_translate_lock` extension downloads all packages and generates a self-contained `@npm` Bazel repository.

### Setup

1. Create a `pnpm-lock.yaml` (use `pnpm install` in your project root).

2. Add to `MODULE.bazel`:

```python
npm = use_extension("@rules_typescript//npm:extensions.bzl", "npm")
npm.translate_lock(pnpm_lock = "//:pnpm-lock.yaml")
use_repo(npm, "npm")
```

3. Reference packages in BUILD files:

```python
ts_compile(
    name = "app",
    srcs = ["app.ts"],
    deps = ["@npm//:zod", "@npm//:react"],
)
```

### Label convention

npm packages map to Bazel labels using this convention:

| npm package | Bazel label |
|-------------|-------------|
| `react` | `@npm//:react` |
| `react-dom` | `@npm//:react-dom` |
| `@types/react` | `@npm//:types_react` |
| `@tanstack/react-query` | `@npm//:tanstack_react-query` |

### Platform-specific packages

The `npm_translate_lock` extension filters out packages whose `os`/`cpu` fields don't match the host machine. This handles packages like `@rollup/rollup-linux-x64-gnu` correctly without manual configuration.

---

## Gazelle

Gazelle auto-generates BUILD files from TypeScript source files, inferring `ts_compile` targets from `index.ts` files and resolving imports to Bazel labels.

### Setup

Add the Gazelle binary to your root BUILD.bazel:

```python
load("@gazelle//:def.bzl", "gazelle")

gazelle(
    name = "gazelle",
    gazelle = "@rules_typescript//gazelle:gazelle_ts",
)
```

Add `gazelle` to MODULE.bazel:

```python
bazel_dep(name = "gazelle", version = "0.47.0")
```

That's all. `rules_typescript` already declares `rules_go`, `go_sdk`, and `go_deps` as non-dev dependencies, so they propagate transitively via bzlmod. Consumers do not need to configure a Go toolchain or Go dependency resolution themselves.

Run Gazelle:

```bash
bazel run //:gazelle
```

### Package boundary heuristic

By default (**every-dir mode**), every directory that contains `.ts` or `.tsx` source files gets a `ts_compile` target. This matches Go's behaviour where every directory with `.go` files is a package.

> **Breaking change from pre-0.2.0 behaviour**: earlier versions used **index-only mode** — only directories with an `index.ts` or `index.tsx` file became package boundaries. If you are upgrading from a workspace that relied on this convention and do not want Gazelle to generate new `ts_compile` targets in leaf directories, add this directive to your root `BUILD.bazel` to restore the old behaviour:
>
> ```python
> # BUILD.bazel (repo root)
> # gazelle:ts_package_boundary index-only
> ```

**every-dir mode** (default): a directory becomes a boundary when it has any `.ts` files.

**index-only mode** (`# gazelle:ts_package_boundary index-only`): a directory becomes a boundary when:

1. It contains an `index.ts` or `index.tsx` file, or
2. It has the `# gazelle:ts_package_boundary true` directive, or
3. It is the repository root.

Test files (`*.test.ts`, `*.spec.ts`, `*.test.tsx`, `*.spec.tsx`) generate `ts_test` targets automatically in both modes.

### Automatic lint targets

When a linter config file is present in the current directory or any ancestor directory, Gazelle automatically generates a `ts_lint` target alongside each `ts_compile` target. The lint target name is the compile target name with `_lint` appended (e.g., `my_lib_lint` for `my_lib`).

Detected config files:
- **oxlint**: `oxlint.json`, `.oxlintrc.json`, `.oxlintrc` — generates `ts_lint(linter = "oxlint", ...)`
- **eslint**: `eslint.config.mjs`, `eslint.config.js`, `.eslintrc.json`, `.eslintrc.*` — generates `ts_lint(linter = "eslint", ...)`

oxlint configs are detected before ESLint configs. The closest config file wins (current directory before ancestors).

Example generated output with an `oxlint.json` at the repo root:

```python
# Generated by Gazelle
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

### gazelle_ts.json

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

`runtimeDeps.test` lists Bazel labels that are appended to every generated `ts_test` deps list. Use this for packages that are needed at test runtime but are never statically imported — for example:

| Package | Why it needs to be explicit |
|---------|----------------------------|
| `@npm//:happy-dom` | vitest environment — imported by vitest config, not your test files |
| `@npm//:react` | JSX runtime (`react/jsx-runtime`) — never directly imported |
| `@npm//:react-dom` | required for React test utilities |
| `@npm//:types_react` | type declarations for JSX |

Without `runtimeDeps.test`, these packages would have to be added manually to each `ts_test` target after Gazelle runs. Setting them once in `gazelle_ts.json` eliminates the most common manual post-Gazelle edit.

### Gazelle directives reference

Directives go in `BUILD.bazel` files as comments:

| Directive | Effect |
|-----------|--------|
| `# gazelle:ts_isolated_declarations false` | Emit `isolated_declarations = False` on all generated `ts_compile` and `ts_test` rules — the escape hatch for existing codebases (see [Path B](#path-b-existing-project-migration)) |
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

---

## Testing with vitest

`ts_test` compiles TypeScript test files and runs them with vitest inside the Bazel sandbox.

### Setup

```python
# BUILD.bazel
load("@rules_typescript//ts:defs.bzl", "ts_compile", "ts_test")
load("@rules_typescript//npm:defs.bzl", "node_modules")

ts_compile(
    name = "math",
    srcs = ["math.ts"],
    visibility = ["//visibility:private"],
)

node_modules(
    name = "node_modules",
    deps = ["@npm//:vitest"],
)

ts_test(
    name = "math_test",
    srcs = ["math.test.ts"],
    deps = [":math", "@npm//:vitest"],
    node_modules = ":node_modules",
)
```

```bash
bazel test //path/to:math_test
```

### ts_test attributes

| Attribute | Type | Default | Description |
|-----------|------|---------|-------------|
| `srcs` | `label_list` | required | `.ts`/`.tsx` test files |
| `deps` | `label_list` | `[]` | `ts_compile` or npm targets the tests import |
| `node_modules` | `label` | `None` | A `node_modules` target for runtime npm resolution |
| `vitest` | `label` | `None` | Explicit vitest binary label (auto-detected from `node_modules` when absent) |
| `runtime` | `label` | `None` | Per-target JS runtime binary override |
| `env` | `string_dict` | `{}` | Additional environment variables |
| `size` | `string` | `"medium"` | Bazel test size |

Vitest is resolved from `node_modules/vitest/dist/cli.js` when not explicitly set.

### ts_test attributes

| Attribute | Type | Default | Description |
|-----------|------|---------|-------------|
| `update_snapshots` | `bool` | `False` | When True, produces an executable that runs `vitest run --update` and writes snapshot files back to the source tree. Use with `bazel run` instead of `bazel test`. |

### Sharding

`ts_test` supports Bazel test sharding. Pass `--test_sharding_strategy=explicit` and set `shard_count` on the target to distribute test files across parallel runners.

### Snapshot testing

Vitest snapshot files (`.snap`) must live in the source tree, but the Bazel sandbox is read-only. There are two supported workflows.

**Recommended: `update_snapshots = True`**

Add a second target to your `BUILD.bazel` that shares the same `srcs` but is marked as a snapshot updater:

```python
ts_test(
    name = "my_test",
    srcs = ["Button.test.tsx"],
    deps = [":button"],
    node_modules = ":node_modules",
)

ts_test(
    name = "update_snapshots",
    srcs = ["Button.test.tsx"],
    deps = [":button"],
    node_modules = ":node_modules",
    update_snapshots = True,  # produces an executable, not a test
)
```

To create or update snapshots:

```bash
bazel run //path/to:update_snapshots
```

Vitest runs with `--update` from the workspace root, so snapshot files are written to the correct `__snapshots__` directory alongside your source files. Commit the resulting `.snap` files to version control.

**Alternative: `--sandbox_writable_path`**

For one-off updates without a dedicated target, grant the sandbox write access to the snapshot directory:

```bash
bazel test //path/to:my_test \
  --sandbox_writable_path=$(pwd)/src/components/__snapshots__
```

Add `--sandbox_writable_path` entries for every snapshot directory that needs updating. This approach works for any test but requires knowing the exact snapshot paths.

### Watch mode

Use [ibazel](https://github.com/bazelbuild/bazel-watcher) to watch test files and re-run them on every change:

```bash
# Install ibazel
go install github.com/bazelbuild/bazel-watcher/cmd/ibazel@latest

# Watch a single test target
ibazel test //path/to:my_test

# Watch all tests in the repo
ibazel test //...
```

ibazel monitors the build graph and re-runs tests whenever a source file or dependency changes. It uses Bazel's file watch capability to avoid full rebuilds — only the affected targets are rebuilt and re-tested.

No custom rule is needed. The standard `ts_test` rule works with ibazel out of the box.

### Debugging

To attach a debugger to vitest running inside the Bazel sandbox, use the `--inspect-brk` flag via the `runtime` attribute or by passing extra arguments:

**Step 1: add a debug target to your BUILD.bazel**

```python
ts_test(
    name = "my_test_debug",
    srcs = ["my.test.ts"],
    deps = [":my_lib"],
    node_modules = ":node_modules",
    tags = ["manual"],  # exclude from bazel test //...
    env = {
        "NODE_OPTIONS": "--inspect-brk=9229",
    },
)
```

**Step 2: run the debug target**

```bash
bazel run //path/to:my_test_debug
```

Vitest starts and pauses before executing any test code, waiting for a debugger to attach on port 9229.

**Step 3: attach VS Code**

Copy `.vscode/launch.json.template` to `.vscode/launch.json` and use the "Debug vitest via --inspect-brk (manual)" configuration to attach.

Alternatively, use the Chrome DevTools debugger:

1. Open `chrome://inspect` in Chrome.
2. Click "Open dedicated DevTools for Node".
3. The debugger attaches to the paused vitest process.

Source maps are configured automatically: Bazel writes `.js.map` files alongside each `.js` output, so VS Code shows the original `.ts` source with correct line numbers.

### Build feedback

To see which targets were (re)built or cached after `bazel build` or `bazel test`, use `--show_result`:

```bash
# Show results for up to 10 targets (default is 1)
bazel test //... --show_result=10

# Show results for all targets (unlimited)
bazel test //... --show_result=0

# Add to .bazelrc for a persistent setting:
# test --show_result=20
```

The output shows each target and whether it passed, failed, was cached (`(cached)`) or skipped. For large monorepos, setting `--show_result=0` in `.bazelrc` ensures you always see a summary of what changed.

---

## Bundling

`ts_binary` and `ts_bundle` collect transitive `.js` outputs and optionally invoke a pluggable bundler.

### Basic usage (placeholder mode)

Without a bundler, the rule concatenates all `.js` files in dependency order. This is useful during development and keeps the build graph valid.

```python
load("@rules_typescript//ts:defs.bzl", "ts_binary")

ts_binary(
    name = "app",
    entry_point = "//src/app",
    format = "esm",
    sourcemap = True,
)
```

```bash
bazel build //:app
```

### With a Vite bundler

```python
load("@rules_typescript//vite:bundler.bzl", "vite_bundler")
load("@rules_typescript//npm:defs.bzl", "node_modules")
load("@rules_typescript//ts:defs.bzl", "ts_bundle")

node_modules(
    name = "node_modules",
    deps = ["@npm//:vite"],
)

vite_bundler(
    name = "vite",
    vite = "@npm//:vite",
    node_modules = ":node_modules",
)

ts_bundle(
    name = "app",
    entry_point = "//src/app",
    bundler = ":vite",
    format = "esm",
    sourcemap = True,
    minify = True,
    external = ["react", "react-dom"],
)
```

### Custom bundler (BundlerInfo interface)

Any Bazel rule that returns `BundlerInfo` can plug into `ts_bundle` and `ts_binary`. This lets you bring your own bundler — esbuild, Rolldown, webpack, or any other tool — without modifying `rules_typescript`.

```python
load("@rules_typescript//ts:defs.bzl", "BundlerInfo")

def _my_bundler_impl(ctx):
    return [BundlerInfo(
        bundler_binary = ctx.file.binary,
        config_file = None,                 # optional static config
        runtime_deps = depset([]),           # files needed at bundle time
        use_generated_config = False,        # set True for Vite-style config
    )]

my_bundler = rule(
    implementation = _my_bundler_impl,
    attrs = {
        "binary": attr.label(
            allow_single_file = True,
            executable = True,
            cfg = "exec",
        ),
    },
)
```

`BundlerInfo` has two invocation modes controlled by `use_generated_config`:

**Mode 1 — Standard CLI** (`use_generated_config = False`, the default)

`ts_bundle` invokes the bundler binary with these positional arguments:

```
<bundler_binary>
  --entry  <path/to/entry.js>
  --out-dir <output/dir>
  --format esm|cjs|iife
  [--external <pkg>]...
  [--sourcemap]
  [--config <config_file>]   (only when config_file is set)
```

Output is expected at `<out-dir>/<bundle_name>.js` (and `.js.map` if `--sourcemap`).

**Mode 2 — Generated config** (`use_generated_config = True`)

`ts_bundle` generates a `vite.config.mjs` containing all bundle options (entry, format, sourcemap, define, external, resolve.alias for Bazel paths) and invokes:

```
<bundler_binary> <absolute_path_to_vite.config.mjs> <entry_path> <out_dir>
```

The bundler binary is responsible for reading the config and running the actual bundler tool. Output file names follow Vite's lib mode convention:

| Format | Output file |
|--------|-------------|
| `esm` | `<bundle_name>.es.js` |
| `cjs` | `<bundle_name>.cjs.js` |
| `iife` | `<bundle_name>.iife.js` |

The generated config includes `resolve.alias` entries mapping exec-root-relative Bazel output paths to absolute paths (using the `EXEC_ROOT` environment variable set by the wrapper script). This ensures Vite can locate compiled `.js` files from the Bazel sandbox without relying on file-system traversal.

**BundlerInfo fields reference**

| Field | Type | Description |
|-------|------|-------------|
| `bundler_binary` | `File` | The executable that performs bundling |
| `config_file` | `File` or `None` | Optional static config passed via `--config` (mode 1 only) |
| `runtime_deps` | `depset of File` | Files the bundler needs at runtime (node_modules, plugins, etc.) |
| `use_generated_config` | `bool` | When `True`, use mode 2 (generated vite.config.mjs) |

### ts_binary / ts_bundle attributes

| Attribute | Type | Default | Description |
|-----------|------|---------|-------------|
| `entry_point` | `label` | required | `ts_compile` target providing `JsInfo` |
| `bundler` | `label` | `None` | Target providing `BundlerInfo` |
| `bundle_name` | `string` | rule name | Output file name (without `.js`) |
| `format` | `string` | `"esm"` | Output format: `esm`, `cjs`, `iife` |
| `sourcemap` | `bool` | `True` | Emit source map |
| `minify` | `bool` | `True` | Minify the bundle (esbuild minifier via Vite) |
| `split_chunks` | `bool` | `False` | Enable chunk splitting (Vite mode only; output is a directory) |
| `external` | `string_list` | `[]` | Module specifiers to leave external |
| `define` | `string_dict` | `{}` | Global constant replacements |

---

## Runtime Toolchain

The JS runtime toolchain is pluggable. It provides the Node.js (or alternative) binary to `ts_test` and `ts_binary`.

### Default Node.js runtime

Node.js is provided automatically by `rules_typescript` via `rules_nodejs`. The `node.toolchain` call in `rules_typescript`'s own `MODULE.bazel` downloads Node.js and registers the toolchains for all supported platforms. Consumers get this for free with `bazel_dep`.

To use a different Node.js version, add a `rules_nodejs` extension call in your own `MODULE.bazel`:

```python
node = use_extension("@rules_nodejs//nodejs:extensions.bzl", "node")
node.toolchain(
    name = "nodejs",
    node_version = "22.14.0",
)
```

Your version takes precedence over the one bundled with `rules_typescript` because bzlmod resolves extension calls from the root module first.

### Custom runtime (Deno, Bun, etc.)

```python
load("@rules_typescript//ts/private:runtime.bzl", "js_runtime_toolchain")

js_runtime_toolchain(
    name = "deno_toolchain",
    runtime_binary = "@deno//:deno",
    runtime_name = "deno",
    args_prefix = ["run", "--allow-all"],
)

toolchain(
    name = "deno",
    toolchain = ":deno_toolchain",
    toolchain_type = "@rules_typescript//ts/toolchain:js_runtime_type",
    target_compatible_with = ["@platforms//os:linux"],
)
```

### Per-target runtime override

```python
ts_test(
    name = "my_test",
    srcs = ["my.test.ts"],
    runtime = "//tools:my_node_wrapper",  # overrides toolchain
)
```

---

## toolchain_utils integration

`rules_typescript` toolchains expose the standard fields required by `@toolchain_utils//toolchain:resolved.bzl`:

| Toolchain | `executable` | `variable` |
|-----------|-------------|------------|
| oxc | `oxc-bazel` binary | `OXC` |
| tsgo | `tsgo` binary | `TSGO` |
| node runtime | `node` binary | `NODE` |

This lets you get resolved toolchain binaries via `toolchain_utils`:

```python
load("@toolchain_utils//toolchain:resolved.bzl", "resolved_toolchain")

resolved_toolchain(
    name = "node_resolved",
    toolchain = "@rules_typescript//ts/toolchain:js_runtime_type",
)
```

---

## Rules Reference

### `ts_compile`

Compiles TypeScript source files using Oxc. Optionally type-checks with tsgo.

```python
load("@rules_typescript//ts:defs.bzl", "ts_compile")

ts_compile(
    name = "lib",
    srcs = ["index.ts", "math.ts"],
    deps = ["//other/package", "@npm//:zod"],
    target = "es2022",
    jsx_mode = "react-jsx",
    isolated_declarations = True,
    enable_check = True,
    visibility = ["//visibility:public"],
)
```

| Attribute | Type | Default | Description |
|-----------|------|---------|-------------|
| `srcs` | `label_list` | required | `.ts`, `.tsx`, or `.d.ts` files |
| `deps` | `label_list` | `[]` | `ts_compile` or `ts_npm_package` targets |
| `target` | `string` | `"es2022"` | ECMAScript target version |
| `jsx_mode` | `string` | `"react-jsx"` | JSX transform: `react-jsx`, `react`, `preserve` |
| `isolated_declarations` | `bool` | `True` | Enable isolated declarations for fast `.d.ts` emit |
| `enable_check` | `bool` | `True` | Run tsgo type-checking (requires tsgo toolchain) |

Type-checking runs as a Bazel validation action in `_validation` output group. It executes during `bazel build` but does not block downstream compilation.

### `ts_test`

Compiles and runs TypeScript tests with vitest. See [Testing with vitest](#testing-with-vitest).

### `ts_binary` / `ts_bundle`

Produces a bundled JavaScript output. See [Bundling](#bundling).

### `ts_dev_server`

Starts a Vite development server that serves compiled JavaScript from `bazel-bin`. Designed to be used with `ibazel` for watch-mode development.

```python
load("@rules_typescript//ts:defs.bzl", "ts_compile", "ts_dev_server")

ts_dev_server(
    name = "dev",
    entry_point = ":app",
    port = 5173,
    plugin = "@rules_typescript//vite:vite_plugin_bazel",
)
```

```bash
ibazel run //src/app:dev
```

The `plugin` attribute wires `vite-plugin-bazel`, which intercepts `.ts` import resolution and redirects to compiled `.js` files in `bazel-bin`. When run with `ibazel`, the plugin also triggers component-level HMR updates on each rebuild instead of full-page reloads.

**Gazelle** automatically sets `plugin = "@rules_typescript//vite:vite_plugin_bazel"` when generating a new `ts_dev_server` target. The attribute is only set on first generation and can be freely removed if you do not use Vite.

| Attribute | Type | Default | Description |
|-----------|------|---------|-------------|
| `entry_point` | `label` | required | `ts_compile` target for the application entry point |
| `port` | `int` | `5173` | Dev server port |
| `host` | `string` | `"localhost"` | Dev server host. Set to `"0.0.0.0"` to bind on all interfaces |
| `open` | `bool` | `False` | Open the browser automatically on start |
| `node_modules` | `label` | `None` | `node_modules` target providing Vite and other runtime deps |
| `plugin` | `label` | `None` | Compiled `.mjs` file for `vite-plugin-bazel` (enables HMR with ibazel) |
| `bundler` | `label` | `None` | `BundlerInfo`-providing target (optional; dev mode uses Vite natively) |

### `ts_check` (advanced)

Standalone type-checking rule that generates a `tsconfig.json` and runs tsgo. Use `ts_compile` with `enable_check = True` instead — `ts_check` is for advanced scenarios where you need a separate check target.

### `ts_npm_publish`

Assembles a publishable npm package from a `ts_compile` target. Collects `.js`, `.js.map`, and `.d.ts` outputs, merges them with a `package.json` template, and produces both a staging directory and a tarball ready for `npm publish`.

```python
load("@rules_typescript//ts:defs.bzl", "ts_compile", "ts_npm_publish")

ts_compile(
    name = "lib",
    srcs = ["index.ts", "math.ts"],
    visibility = ["//visibility:public"],
)

ts_npm_publish(
    name = "lib_pkg",
    package = ":lib",
    package_json = ":package.json",
    version = "1.2.3",
)
```

```bash
bazel build //:lib_pkg
```

Two outputs are produced:

| Output | Description |
|--------|-------------|
| `lib_pkg_pkg/` | Staging directory with all files at package root |
| `lib_pkg_pkg.tar` | Tarball with `package/` prefix (ready for `npm publish`) |

Publish directly:

```bash
npm publish $(bazel cquery --output=files //:lib_pkg | grep '\.tar$')
```

| Attribute | Type | Default | Description |
|-----------|------|---------|-------------|
| `package` | `label` | required | `ts_compile` target providing `JsInfo` and `TsDeclarationInfo` |
| `package_json` | `label` | required | `package.json` template file |
| `version` | `string` | `""` | If set, overrides the `version` field in `package.json` |

The `package.json` template should specify `"main"`, `"types"`, and `"exports"` fields pointing to the compiled output files. The rule does not modify these fields; they must already reference the correct filenames.

---

## Platform Support

`rules_typescript` supports the following platforms:

| Platform | oxc-bazel | tsgo |
|----------|-----------|------|
| Linux x86_64 | built from source (rules_rust) | `@typescript/native-preview-linux-x64` |
| Linux ARM64 | built from source (rules_rust) | `@typescript/native-preview-linux-arm64` |
| macOS x86_64 | built from source (rules_rust) | `@typescript/native-preview-darwin-x64` |
| macOS ARM64 | built from source (rules_rust) | `@typescript/native-preview-darwin-arm64` |

`oxc-bazel` is a Rust binary built from source by `rules_rust`. This means it builds natively for whatever exec platform is running Bazel — no pre-built binaries are needed.

`tsgo` is downloaded from the `@typescript/native-preview-<platform>` npm packages.

### Container Builds (Bazel-in-Docker)

Bazel works correctly inside Docker containers without privileged mode. The standard approach:

```dockerfile
FROM ubuntu:24.04

# Install Bazel prerequisites
RUN apt-get update && apt-get install -y \
    curl \
    git \
    python3 \
    && rm -rf /var/lib/apt/lists/*

# Install Bazelisk (manages Bazel version from .bazelversion)
RUN curl -Lo /usr/local/bin/bazel \
    https://github.com/bazelbuild/bazelisk/releases/latest/download/bazelisk-linux-amd64 \
    && chmod +x /usr/local/bin/bazel

WORKDIR /workspace
COPY . .

# Build and test
RUN bazel build //...
RUN bazel test //...
```

Key points for containers:

- Mount a Bazel cache volume to avoid re-downloading toolchains on each run:

  ```bash
  docker run -v bazel-cache:/root/.cache/bazel my-image bazel build //...
  ```

- The Rust toolchain for `oxc-bazel` is the largest download on the first build (roughly 500 MB). Subsequent builds use the cache.
- Sandbox mode works without `--privileged` on Linux. The default `linux-sandbox` strategy is used automatically.
- For ARM64 containers (e.g., Apple Silicon CI runners), all toolchains are available: `rules_rust` builds `oxc-bazel` for `aarch64-unknown-linux-gnu` natively, and `@typescript/native-preview-linux-arm64` provides `tsgo`.

---

## Monorepo Layout

`rules_typescript` is designed for monorepos. The recommended layout:

```
my-monorepo/
├── MODULE.bazel
├── pnpm-lock.yaml          # single lockfile for all packages
├── packages/
│   ├── ui/
│   │   ├── BUILD.bazel     # ts_compile(name = "ui", ...)
│   │   └── index.ts
│   ├── utils/
│   │   ├── BUILD.bazel     # ts_compile(name = "utils", ...)
│   │   └── index.ts
│   └── config/
│       ├── BUILD.bazel
│       └── index.ts
└── apps/
    └── server/
        ├── BUILD.bazel     # ts_compile that depends on //packages/ui, //packages/utils
        └── main.ts
```

### Package Boundaries

A directory should have its own `ts_compile` target when:

1. It has an `index.ts` that forms a public API (Gazelle auto-detects this).
2. Other packages import from it — cross-package imports must go through the `ts_compile` target.
3. It will be published as a separate npm package.

```python
# packages/utils/BUILD.bazel
load("@rules_typescript//ts:defs.bzl", "ts_compile")

ts_compile(
    name = "utils",
    srcs = ["index.ts", "string.ts", "number.ts"],
    visibility = ["//visibility:public"],  # allow other packages to depend on this
)
```

```python
# apps/server/BUILD.bazel
load("@rules_typescript//ts:defs.bzl", "ts_compile")

ts_compile(
    name = "server",
    srcs = ["main.ts"],
    deps = [
        "//packages/utils",
        "//packages/ui",
        "@npm//:express",
    ],
)
```

### Using Gazelle for BUILD File Generation

Run Gazelle once to generate BUILD files for the entire monorepo:

```bash
bazel run //:gazelle
```

Gazelle creates `ts_compile` targets for every directory with an `index.ts`, resolves import paths to Bazel labels, and generates `ts_test` targets for test files. After adding new source files or packages, re-run Gazelle to update BUILD files.

---

## Cross-Package Dependencies

`.d.ts` files are the compilation boundary between packages:

```python
# //lib/BUILD.bazel
ts_compile(
    name = "lib",
    srcs = ["math.ts"],
    visibility = ["//visibility:public"],
)

# //app/BUILD.bazel
ts_compile(
    name = "app",
    srcs = ["main.ts"],
    deps = ["//lib"],
)
```

If `lib/math.ts` changes but its exported types don't change, `app` is not recompiled. Bazel's content-based caching uses the `.d.ts` fingerprint as the dependency boundary.

---

## Migration from rules_ts

`rules_typescript` is a fresh implementation, not a drop-in replacement for `rules_ts` from `aspect-build`. Key differences:

| | rules_ts (aspect-build) | rules_typescript (this) |
|--|------------------------|------------------------|
| Compiler | tsc | Oxc (Rust) |
| Type-checker | tsc (compile + check) | tsgo (check only) |
| .d.ts boundary | requires ts_project per file | automatic per source file |
| npm | aspect_rules_js | built-in pnpm lockfile parsing |
| Gazelle | not included | built-in |

Migration steps:

1. Replace `ts_project` targets with `ts_compile`.
2. Replace `js_library` / `npm_link_all_packages` with `npm_translate_lock`.
3. Remove `tsconfig.json` from BUILD deps — `ts_compile` generates its tsconfig internally.
4. Run `bazel run //:gazelle` to regenerate BUILD files for new source structure.
5. If your codebase lacks explicit return types on exports, add `# gazelle:ts_isolated_declarations false` to your root `BUILD.bazel` first (see [Path B](#path-b-existing-project-migration)), then migrate package by package.

---

## Troubleshooting

### Type errors not surfacing

Type-checking runs only when a tsgo toolchain is registered and `enable_check = True` (both are defaults). The recommended way to enable it permanently is to add the following line to your `.bazelrc`:

```
build --output_groups=+_validation
```

To trigger it for a single build without modifying `.bazelrc`:

```bash
bazel build //... --output_groups=+_validation
```

### tsgo not found

The tsgo toolchain is registered automatically by `rules_typescript`. If it is not resolving, confirm that your `bazel_dep` for `rules_typescript` is present and that no explicit `register_toolchains` call in your workspace is shadowing the defaults.

To use a specific tsgo version, add an extension call:

```python
ts = use_extension("@rules_typescript//ts:extensions.bzl", "ts")
ts.tsgo(version = "7.0.0-dev.20260311.1")
```

### Import not resolving in tsgo

tsgo uses `moduleResolution: "Bundler"` with `paths` entries for direct npm deps. If tsgo cannot resolve a bare import like `import { z } from "zod"`, add the package as a direct dep:

```python
ts_compile(
    name = "app",
    srcs = ["app.ts"],
    deps = ["@npm//:zod"],  # must be here, not just a transitive dep
)
```

### vitest not found at test runtime

The `node_modules` target must include vitest:

```python
node_modules(
    name = "node_modules",
    deps = ["@npm//:vitest"],
)

ts_test(
    name = "my_test",
    node_modules = ":node_modules",
    ...
)
```

### Isolated declarations error: missing return type

When `isolated_declarations = True`, all exported functions and variables must have explicit type annotations. The tsgo type-checker will report:

```
error TS9007: Declaration emit for this file requires type resolution. ...
```

Add the missing return type or explicit type annotation to the export.

### Gazelle generating wrong deps

If Gazelle generates incorrect `deps` for an import:

1. Check that the import specifier matches an npm package name in the lockfile.
2. For path aliases, verify `gazelle_ts.json` has the correct `pathAliases` entries.
3. Use `# gazelle:ts_ignore` to suppress generation for a directory and write its BUILD file manually.

### Slow first build

The first build downloads: the Rust toolchain (for oxc_cli), tsgo npm tarballs, Node.js tarballs, and all npm packages from the lockfile. Subsequent builds are fully cached.

To verify that the first build works from a clean state with only Bazelisk installed, run the quickstart script from the `rules_typescript` source tree:

```bash
bash scripts/quickstart.sh
```

This creates a minimal workspace in a temporary directory, runs `bazel build //...`, and confirms that all toolchains are fetched and compilation succeeds.

---

## Project Structure

```
rules_typescript/
├── MODULE.bazel          # bzlmod module definition
├── ts/
│   ├── defs.bzl          # public API: ts_compile, ts_test, ts_binary, ts_bundle, BundlerInfo
│   ├── extensions.bzl    # module extension for ts/node/tsgo toolchains
│   ├── repositories.bzl  # repository rule re-exports
│   ├── toolchain/        # toolchain_type declarations
│   └── private/          # rule implementations
│       ├── ts_compile.bzl      # core compile + validation rule
│       ├── ts_test.bzl         # vitest test runner
│       ├── ts_binary.bzl       # bundle rule (stable public name)
│       ├── ts_bundle.bzl       # bundle rule (canonical name, shared impl)
│       ├── ts_check.bzl        # standalone type-check rule
│       ├── ts_config_gen.bzl   # standalone tsconfig generator
│       ├── toolchain.bzl       # toolchain providers + repo rules
│       ├── runtime.bzl         # JS runtime toolchain
│       ├── node_modules.bzl    # node_modules tree builder
│       ├── npm_translate_lock.bzl # pnpm lockfile parser + downloader
│       ├── ts_npm_package.bzl  # npm package target rule
│       └── ts_npm_publish.bzl  # ts_npm_publish rule for publishing packages
│       └── providers.bzl       # JsInfo, TsDeclarationInfo, BundlerInfo, NpmPackageInfo
├── npm/                  # npm extension (npm_translate_lock wrapper)
│   ├── extensions.bzl
│   └── defs.bzl
├── oxc_cli/              # Rust binary (oxc-bazel)
├── gazelle/              # Gazelle extension (Go)
│   ├── language.go       # extension entry point
│   ├── config.go         # directives + gazelle_ts.json
│   ├── generate.go       # BUILD file generation
│   ├── resolve.go        # import → label resolution
│   └── imports.go        # TypeScript import extraction
├── vite/                 # Vite bundler integration
│   └── bundler.bzl       # vite_bundler rule
├── tests/                # in-repo tests
│   ├── smoke/            # minimal .ts/.tsx compilation
│   ├── multi/            # cross-package deps
│   ├── vitest/           # ts_test with real vitest run
│   └── bundle/           # ts_binary bundling
└── e2e/basic/            # end-to-end workspace with local_path_override
```

## Architecture

```
                    ┌──────────────┐
                    │  .ts source  │
                    └──────┬───────┘
                           │
              ┌────────────┼────────────┐
              │            │            │
              ▼            ▼            ▼
          ┌──────┐    ┌────────┐   ┌───────┐
          │ .js  │    │ .js.map│   │ .d.ts │  ← compilation boundary
          └──────┘    └────────┘   └───┬───┘
                                       │
                              downstream deps
                              only see .d.ts
```

The oxc-bazel binary processes each `.ts` file through:

1. Parse (oxc_parser)
2. Semantic analysis (oxc_semantic)
3. Isolated declarations emit (oxc_isolated_declarations) — **before** transform
4. TypeScript/JSX transform (oxc_transformer)
5. Code generation (oxc_codegen) for `.js` + `.js.map`

tsgo type-checking runs as a Bazel validation action: it uses a generated `tsconfig.json` with `rootDirs` bridging the source tree and output tree, `moduleResolution: "Bundler"`, and `--noEmit`.

## License

MIT
