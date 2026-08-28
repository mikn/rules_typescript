# Quick Start

The only prerequisite is **Bazelisk** (or Bazel 9+ directly). Everything else — the Rust toolchain, Go toolchain, Node.js runtime, and all npm packages — is fetched hermetically by Bazel on the first build.

The first build fetches a Rust toolchain, a Go SDK, Node.js and tsgo, and then
**compiles `oxc-bazel` from Rust source**. That compile dominates: expect
minutes, and expect it to scale with your machine rather than your project.
Everything after it is cached; small changes rebuild in milliseconds.

Choose your path:

- [Depending on rules_typescript](#depending-on-rules_typescript) — how to pin the ruleset before it reaches the Bazel Central Registry
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

There is no Bazel Central Registry entry and no tagged release yet, so a bare
`bazel_dep(name = "rules_typescript", version = "0.2.0")` has nothing to
resolve against. Until the ruleset is published to the BCR, pin it with a
non-registry override. All three forms below keep `bazel_dep` in place —
bzlmod still requires the `version` attribute, and ignores its value while an
override is active.

### git_override — the pre-BCR default

```python
bazel_dep(name = "rules_typescript", version = "0.2.0")
git_override(
    module_name = "rules_typescript",
    remote = "https://github.com/mikn/rules_typescript.git",
    commit = "REPLACE_WITH_A_COMMIT_SHA_FROM_MAIN",
)
```

Pin a full 40-character commit SHA rather than a branch name: `git_override`
re-resolves a branch whenever the repository cache is cold, which makes the
build non-reproducible.

### archive_override — smaller fetch

`git_override` runs a full `git clone` and pays for the whole history, which
still carries ~200 MB of cargo build output that was tracked by mistake before
it was removed. A codeload tarball is a single snapshot instead — under 1 MB —
so prefer this form on CI. Compute the integrity hash for the commit you want:

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

### local_path_override — working against a checkout

```python
bazel_dep(name = "rules_typescript", version = "0.2.0")
local_path_override(
    module_name = "rules_typescript",
    path = "../rules_typescript",
)
```

Once a version is published to the BCR, drop the override and the plain
`bazel_dep` line resolves on its own.

---

## Path A: New Project

**Step 1.** Create `.bazelversion`:

```
9.0.0
```

**Step 2.** Create `WORKSPACE.bazel` (empty file — required by Bazel 9):

```
```

**Step 3.** Create `MODULE.bazel`. `rules_typescript` is not on the Bazel
Central Registry yet, so pin it from git with `git_override` — see
[Depending on rules_typescript](#depending-on-rules_typescript) above for the
full explanation and the `archive_override` alternative:

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

That is the whole file. In particular you do **not** need any
`@rules_rust//...` flag: `rules_rust` is a transitive dependency of
`rules_typescript`, not of your module, so `@rules_rust` is not visible from
your repository and Bazel rejects the flag outright (see
[Troubleshooting](../guides/troubleshooting.md#no-repository-visible-as-rules_rust)).

**Step 5.** Create `BUILD.bazel` at the repo root. It may be empty, but it has
to exist — `rules_rust`'s crate fetching resolves `//:MODULE.bazel`, which
requires the repo root to be a Bazel package:

```python
load("@gazelle//:def.bzl", "gazelle")

gazelle(
    name = "gazelle",
    gazelle = "@rules_typescript//gazelle:gazelle_ts",
)
```

**Step 6.** Write your TypeScript files. Explicit return types are optional —
tsgo emits the declarations from the full type program, so an inferred one is
fine:

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

Each `ts_compile` target Gazelle generates produces `.js`, `.js.map`, and `.d.ts`
outputs per source file — `bazel-bin/src/lib/math.js`, `math.js.map` and
`math.d.ts` for the file above.

**Step 9.** Run tests — once there is one. A project with no `*.test.ts` yet has
no test target, and Bazel treats that as an error rather than a no-op:

```
$ bazel test //...
INFO: Found 2 targets and 0 test targets...
ERROR: No test targets were found, yet testing was requested
```

That is Bazel's exit code 4, not a broken setup. Writing the first test needs
vitest, which comes from your lockfile rather than from the ruleset, so it needs
the npm setup below first:

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

`"pnpm"` is not optional: Gazelle writes a `ts_pnpm` and a `ts_add_package`
target into your root `BUILD.bazel` as soon as a `pnpm-lock.yaml` exists, and
both name `@pnpm`. Leave it out and `bazel build //...` aborts with
`No repository visible as '@pnpm' from main repository`
([why](../guides/npm.md#setup)).

Then write the test beside the source, re-run Gazelle, and test:

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

The full story — DOM environments, coverage, snapshots, sharding — is in
[Testing with vitest](../guides/testing.md).

---

## Path B: Existing Project

**Step 1.** Set up the same four root files as Path A (`.bazelversion`, `WORKSPACE.bazel`, `MODULE.bazel`, `.bazelrc`).

**Step 2.** Create `BUILD.bazel` at the repo root. No escape hatch is needed:

```python
load("@gazelle//:def.bzl", "gazelle")

gazelle(
    name = "gazelle",
    gazelle = "@rules_typescript//gazelle:gazelle_ts",
)
```

Most existing TypeScript projects do not annotate every export, and that is
fine: the `ts_compile` default emits declarations with tsgo, which infers them.

**Step 3.** Wire up your `pnpm-lock.yaml`. Do this *before* the first build:
Gazelle resolves every bare import in your sources to an `@npm//:…` label, so a
project with any npm dependency at all fails analysis with
`No repository visible as '@npm' from main repository` until the hub exists.

```python
# MODULE.bazel
npm = use_extension("@rules_typescript//npm:extensions.bzl", "npm")
npm.translate_lock(pnpm_lock = "//:pnpm-lock.yaml")
use_repo(npm, "npm", "pnpm")
```

Both names are needed. `@npm` is the alias hub your `deps` labels spell; `@pnpm`
backs the `ts_pnpm` and `ts_add_package` targets Gazelle writes into your root
`BUILD.bazel` the moment it sees a lockfile. Details, including private
registries and patched dependencies:
[npm Dependencies](../guides/npm.md).

No `pnpm install` is needed, then or later — the lockfile is the only npm input.

**Step 4.** Run Gazelle:

```bash
bazel run //:gazelle
```

**Step 5.** Build everything:

```bash
bazel build //...
```

If there are type errors, fix them — real type errors fail the build, because
the `.d.ts` are outputs of the type-checker. You will not see "missing return
type" errors: those only apply to `declarations = "oxc"`.

!!! warning "A `compilerOptions.paths` alias that crosses a target boundary"
    Gazelle reads `compilerOptions.paths` out of your `tsconfig.json` and writes
    a matching `path_aliases` attr on the targets whose imports go through it.
    `ts_compile` accepts an alias only when it resolves to files *that target*
    stages, so the near-universal `"@/*": ["src/*"]` — where `@/lib/math` is
    produced by another package — fails at analysis:

    ```
    ts_compile: path_aliases["@/"] on @@//src/app:app points at "./src/", where
    none of this target's inputs live.
    ```

    Cross-package imports are `module_name`'s job, not `path_aliases`'. Set it on
    the producing target and drop the alias from the consumer — with a `# keep`
    above the rule, because Gazelle re-derives the attr from `tsconfig.json` on
    every run:

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
Oxc's syntactic declaration emit to take type-checking off the critical path.
See [Isolated Declarations](isolated-declarations.md).

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
`rules_typescript` asks for — and `rules_typescript`'s toolchains resolve the
repositories that name generates (`nodejs_linux_amd64` and friends). Under any
other name your registration is unused, with nothing reporting it.
