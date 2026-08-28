# Quick Start

The only prerequisite is **Bazelisk** (or Bazel 9+ directly). Bazel fetches
everything else hermetically on the first build: the Rust toolchain, the Go
SDK, the Node.js runtime, tsgo, and the npm packages your targets reach. It
also compiles `oxc-bazel` from Rust source, which dominates the wall time:
expect minutes at any project size. After that everything is cached, and small
changes rebuild in milliseconds.

Choose your path:

- [Depending on rules_typescript](#depending-on-rules_typescript) — pinning the ruleset before it reaches the Bazel Central Registry
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

## Depending on rules_typescript

`rules_typescript` has no Bazel Central Registry entry and no tagged release
yet, so a bare `bazel_dep(name = "rules_typescript", version = "0.2.0")`
resolves against nothing. Pin it with a non-registry override. All three forms
below keep the `bazel_dep` line, which is what makes the module a direct
dependency. bzlmod ignores the `version` value while a non-registry override is
active, and accepts the line with no `version` at all.

### git_override

```python
bazel_dep(name = "rules_typescript", version = "0.2.0")
git_override(
    module_name = "rules_typescript",
    remote = "https://github.com/mikn/rules_typescript.git",
    commit = "REPLACE_WITH_A_COMMIT_SHA_FROM_MAIN",
)
```

Use a full 40-character commit SHA. `git_override` re-resolves a branch name
whenever the repository cache is cold, which makes the build non-reproducible.

### archive_override

`git_override` runs a full `git clone` and pays for the whole history, which
still carries ~200 MB of cargo build output that was tracked by mistake before
it was removed. A codeload tarball is a single snapshot of about 1.3 MB; prefer
it on CI. Compute the integrity hash for the commit you want:

```bash
COMMIT=<full 40-char sha>
curl -sL "https://github.com/mikn/rules_typescript/archive/$COMMIT.tar.gz" \
  | openssl dgst -sha256 -binary | openssl base64 -A
```

```python
bazel_dep(name = "rules_typescript", version = "0.2.0")
archive_override(
    module_name = "rules_typescript",
    urls = ["https://github.com/mikn/rules_typescript/archive/<sha>.tar.gz"],
    strip_prefix = "rules_typescript-<sha>",
    integrity = "sha256-<base64 output of the command above>",
)
```

### local_path_override

For a checkout on disk:

```python
bazel_dep(name = "rules_typescript", version = "0.2.0")
local_path_override(
    module_name = "rules_typescript",
    path = "../rules_typescript",
)
```

Once a version is published to the BCR, drop the override; the plain
`bazel_dep` line resolves on its own.

---

## Path A: New Project

**Step 1.** Create `.bazelversion`:

```
9.0.0
```

**Step 2.** Create an empty `WORKSPACE.bazel` (optional — `MODULE.bazel` marks
the root in Bazel 9; every workspace in this repo carries one):

```
```

**Step 3.** Create `MODULE.bazel`, pinning `rules_typescript` with
`git_override` (see
[Depending on rules_typescript](#depending-on-rules_typescript) for the
`archive_override` alternative):

```python
module(
    name = "my_project",
    version = "0.0.0",
)

bazel_dep(name = "rules_typescript", version = "0.2.0")
git_override(
    module_name = "rules_typescript",
    remote = "https://github.com/mikn/rules_typescript.git",
    commit = "REPLACE_WITH_A_COMMIT_SHA_FROM_MAIN",
)

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

`rules_rust` reaches your build as a transitive dependency of
`rules_typescript`, so `@rules_rust` is not visible from your repository: a
`@rules_rust//...` flag here is rejected outright (see
[Troubleshooting](../guides/troubleshooting.md#no-repository-visible-as-rules_rust)).

**Step 5.** Create `BUILD.bazel` at the repo root. It has to exist even when
empty: `rules_rust`'s crate fetching resolves `//:MODULE.bazel`, which requires
the repo root to be a Bazel package:

```python
load("@gazelle//:def.bzl", "gazelle")

gazelle(
    name = "gazelle",
    gazelle = "@rules_typescript//gazelle:gazelle_ts",
)
```

**Step 6.** Write your TypeScript files. Explicit return types are optional;
tsgo emits the declarations from the full type program:

```typescript
// src/lib/math.ts
export function add(a: number, b: number) {
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

Each `ts_compile` target Gazelle generates produces `.js`, `.js.map`, and
`.d.ts` per source file: `bazel-bin/src/lib/math.js`, `math.js.map` and
`math.d.ts` for the file above.

**Step 9.** Run tests, once there is one. With no `*.test.ts` there is no test
target, and Bazel treats that as an error:

```
$ bazel test //...
INFO: Found 2 targets and 0 test targets...
ERROR: No test targets were found, yet testing was requested
```

The exit code is 4. vitest comes from your lockfile, so the first test needs
the npm setup:

```bash
pnpm init
pnpm add vitest --lockfile-only
```

```python
# MODULE.bazel — add to what Step 3 wrote
npm = use_extension("@rules_typescript//npm:extensions.bzl", "npm")
npm.translate_lock(pnpm_lock = "//:pnpm-lock.yaml")
use_repo(npm, "npm", "pnpm")
```

`"pnpm"` is not optional: Gazelle writes `ts_pnpm` and `ts_add_package`
targets naming `@pnpm` into your root `BUILD.bazel` as soon as a
`pnpm-lock.yaml` exists. Leave it out and `bazel build //...` aborts with
`No repository visible as '@pnpm' from main repository`. See
[npm Dependencies](../guides/npm.md#setup).

Write the test beside the source, re-run Gazelle, and test:

```typescript
// src/lib/math.test.ts
import { expect, it } from "vitest";

import { add } from "./math";

it("adds", () => {
  expect(add(2, 3)).toBe(5);
});
```

```bash
bazel run //:gazelle    # writes ts_test(name = "lib_test", ...)
bazel test //...        # //src/lib:lib_test  PASSED
```

See [Testing with vitest](../guides/testing.md) for DOM environments,
coverage, snapshots and sharding.

---

## Path B: Existing Project

**Step 1.** Set up the same root files as Path A: `.bazelversion`,
`MODULE.bazel`, `.bazelrc`, and the optional `WORKSPACE.bazel`.

**Step 2.** Create `BUILD.bazel` at the repo root. No escape hatch is needed:

```python
load("@gazelle//:def.bzl", "gazelle")

gazelle(
    name = "gazelle",
    gazelle = "@rules_typescript//gazelle:gazelle_ts",
)
```

Explicit return types stay optional: the `ts_compile` default emits
declarations with tsgo, which infers them.

**Step 3.** Wire up your `pnpm-lock.yaml` before the first build. Gazelle
resolves every bare import to an `@npm//:…` label, so a project with any npm
dependency fails analysis with
`No repository visible as '@npm' from main repository` until the hub exists.

```python
# MODULE.bazel
npm = use_extension("@rules_typescript//npm:extensions.bzl", "npm")
npm.translate_lock(pnpm_lock = "//:pnpm-lock.yaml")
use_repo(npm, "npm", "pnpm")
```

Both names are needed: `@npm` is the alias hub your `deps` labels spell, and
`@pnpm` backs the `ts_pnpm` and `ts_add_package` targets Gazelle writes into
your root `BUILD.bazel`. See [npm Dependencies](../guides/npm.md) for private
registries and patched dependencies.

`pnpm install` is never needed: the lockfile is the only npm input.

**Step 4.** Run Gazelle:

```bash
bazel run //:gazelle
```

**Step 5.** Build everything:

```bash
bazel build //...
```

Type errors fail the build, because the `.d.ts` are outputs of the
type-checker. "Missing return type" errors apply only to
`declarations = "oxc"`.

!!! warning "A `compilerOptions.paths` alias that crosses a target boundary"
    Gazelle reads `compilerOptions.paths` from your `tsconfig.json` and writes
    a matching `path_aliases` attr on the targets whose imports go through it.
    `ts_compile` accepts an alias only when it resolves to files the same target
    stages, so `"@/*": ["src/*"]` fails at analysis when `@/lib/math` comes from
    another package:

    ```
    ts_compile: path_aliases["@/"] on @@//src/app:app points at "./src/", where
    none of this target's inputs live.
    ```

    Cross-package imports are `module_name`'s job. Set it on the producing
    target and drop the alias from the consumer, with a `# keep` above the
    rule, since Gazelle re-derives the attr from `tsconfig.json` on every run:

    ```python
    # src/lib/BUILD.bazel
    ts_compile(
        name = "lib",
        srcs = ["math.ts"],
        module_name = "@/lib",
        visibility = ["//visibility:public"],
    )

    # src/app/BUILD.bazel
    # keep
    ts_compile(
        name = "app",
        srcs = ["main.ts"],
        deps = ["//src/lib"],
    )
    ```

    Your sources and your editor keep importing `@/lib/math` unchanged. See
    [importing another target by bare specifier](../rules/ts-compile.md#importing-another-target-by-bare-specifier).

**Step 6.** Optional. Once a package's exports are all annotated, move it to
Oxc's syntactic declaration emit, which takes type-checking off the critical
path. See [Isolated Declarations](isolated-declarations.md).

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

Keep `name = "nodejs"`. `rules_nodejs` keeps the root module's registration of
that name and ignores every other module's, so your version wins over the one
`rules_typescript` asks for. `rules_typescript`'s toolchains resolve the
repositories that name generates (`nodejs_linux_amd64` and friends). Under any
other name your registration is silently unused.
