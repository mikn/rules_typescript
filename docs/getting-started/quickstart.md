# Quick Start

The only prerequisite is **Bazelisk** (or Bazel 9+ directly). Everything else — the Rust toolchain, Go toolchain, Node.js runtime, and all npm packages — is fetched hermetically by Bazel on the first build.

The first build fetches all toolchains — typically 2-5 minutes. Subsequent builds are fully cached and take milliseconds for small changes.

Choose your path:

- [Path A: New project](#path-a-new-project) — starting from scratch
- [Path B: Existing project](#path-b-existing-project) — migrating a TypeScript codebase

---

## Install Bazelisk

Bazelisk reads `.bazelversion` and downloads the correct Bazel version automatically.

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

---

## Path A: New Project

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

**Step 6.** Write your TypeScript files. Use explicit return types on exported functions (see [Isolated Declarations](isolated-declarations.md)):

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

Each `ts_compile` target Gazelle generates produces `.js`, `.js.map`, and `.d.ts` outputs per source file.

---

## Path B: Existing Project

**Step 1.** Set up the same four root files as Path A (`.bazelversion`, `WORKSPACE.bazel`, `MODULE.bazel`, `.bazelrc`).

**Step 2.** Create `BUILD.bazel` at the repo root with an escape-hatch directive:

```python
load("@gazelle//:def.bzl", "gazelle")

# gazelle:ts_isolated_declarations false

gazelle(
    name = "gazelle",
    gazelle = "@rules_typescript//gazelle:gazelle_ts",
)
```

The `# gazelle:ts_isolated_declarations false` directive tells Gazelle to set `isolated_declarations = False` on all generated targets. Most existing TypeScript projects don't have explicit return types on every export, so this lets your code compile immediately. You still get hermetic builds and caching — just not the maximum incremental speed. See [Isolated Declarations](isolated-declarations.md) for how to migrate packages one at a time.

**Step 3.** Run Gazelle:

```bash
bazel run //:gazelle
```

**Step 4.** Build everything:

```bash
bazel build //...
```

If there are type errors, fix them. The `isolated_declarations = False` flag means you won't hit "missing return type" errors yet.

**Step 5.** Migrate packages to isolated declarations one at a time. See [Isolated Declarations — Migration](isolated-declarations.md#migration).

---

## Version Pinning

The `ts` extension lets you pin specific tool versions. Add to `MODULE.bazel`:

```python
# Pin tsgo to a specific release. The root module's value wins.
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
