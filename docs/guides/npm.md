# npm Dependencies

npm packages come from a `pnpm-lock.yaml`. The `npm` module extension reads that
file — text only, no network — and declares **one external repository per
package**, plus an alias hub named `@npm` that holds nothing but aliases. Bazel
fetches a package's repository the first time something needs it, so a target's
npm cost is its own dependency closure rather than the whole lockfile.

## Setup

**Step 1.** Create a `pnpm-lock.yaml`:

```bash
pnpm init
pnpm add react react-dom --lockfile-only
```

`--lockfile-only` updates the lockfile without creating a `node_modules/`
directory. No `node_modules/` ever exists in the source tree — Bazel materialises
one inside the sandbox for the targets that need it.

If you would rather not install pnpm at all, see
[Hermetic pnpm](#hermetic-pnpm) below.

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

| npm package | Bazel label |
|-------------|-------------|
| `react` | `@npm//:react` |
| `react-dom` | `@npm//:react-dom` |
| `@types/react` | `@npm//:types_react` |
| `@tanstack/react-query` | `@npm//:tanstack_react-query` |

- Scoped packages (`@scope/name`) become `scope_name` — drop the `@`, replace
  `/` with `_`.
- Hyphens are kept as-is.
- A bare label means the **highest version** of that package in the lockfile.
  When a package resolves at several versions, every version additionally gets a
  version-suffixed label, so you can pin one deliberately.
- A `workspace:*` link resolves to the target in your own repository, not to a
  downloaded package.

## Adding Dependencies

```bash
pnpm add zod --lockfile-only   # updates pnpm-lock.yaml only — no node_modules
bazel run //:gazelle           # Gazelle sees the new import, adds @npm//:zod
bazel build //...              # Bazel fetches just that package's closure
```

`pnpm` is needed only to edit the lockfile. It is not needed at build time, test
time, or on CI.

## Hermetic pnpm

The extension also downloads a standalone pnpm binary, so lockfile edits need no
system install. It is not wired up by default — add the two macros to your root
`BUILD.bazel` and take the repo in `MODULE.bazel`:

```python
# MODULE.bazel
npm = use_extension("@rules_typescript//npm:extensions.bzl", "npm")
npm.pnpm(version = "10.32.1")   # optional; a default version is used otherwise
npm.translate_lock(pnpm_lock = "//:pnpm-lock.yaml")
use_repo(npm, "npm", "pnpm")
```

```python
# BUILD.bazel
load("@rules_typescript//ts:defs.bzl", "ts_add_package", "ts_pnpm")

ts_pnpm(name = "pnpm")
ts_add_package(name = "add_package")
```

```bash
bazel run //:pnpm -- --version
bazel run //:pnpm -- add zod --lockfile-only
bazel run //:add_package -- zod          # appends --lockfile-only for you
```

Both targets `cd` to `$BUILD_WORKSPACE_DIRECTORY` first, so they edit the source
tree rather than a sandbox. The wrapper is a bash script and the binary is
published for Linux and macOS only, so this does not work on Windows.

## Patched dependencies

pnpm's `patchedDependencies` used to be ignored silently: the `packages:`
integrity in the lockfile is the byte-identical upstream tarball, so a patched
package was fetched **unpatched** and nothing said so.

Patches now have to be passed as labels, because the paths pnpm keeps in
`pnpm-workspace.yaml` cannot be turned into labels by an extension — a path like
`patches/foo.patch` says nothing about where your Bazel package boundaries fall:

```python
npm.translate_lock(
    pnpm_lock = "//:pnpm-lock.yaml",
    patches = ["//patches:@acme__diffs@1.3.1.patch"],
)
```

Each file is matched to its lockfile entry by filename, which is pnpm's own
convention from `pnpm patch-commit`:
`<name with / replaced by __>@<version>.patch`.

Every pairing is then **verified when the extension runs**, not when the patched
package happens to enter a build's closure — so a patch that nothing currently
depends on is checked too. Four failures, each naming the label:

- **the label resolves to no readable file.** A label the extension cannot read
  is a patch nothing applies, and the package installs as published — exactly
  what the lockfile says it must not be. Resolving the label also forces the
  patch's Bazel package to load, which is how a broken `patches/BUILD.bazel`
  surfaces here rather than as a mystery later;
- **the file's sha256 disagrees with the digest `patchedDependencies` records.**
  pnpm writes that digest when it writes the patch, so a disagreement means the
  patch changed without `pnpm install` being re-run, and Bazel would apply a
  patch pnpm never saw. A pre-pnpm-9 lockfile records something other than a
  sha256; the file still has to be readable, only the comparison is skipped;
- **a `patchedDependencies` entry with no matching label**;
- **a passed patch file no entry claims** — the lockfile is stale, or the file
  is misnamed.

!!! warning "A patch file whose name starts with `@`"
    `exports_files(glob(["*.patch"]))` cannot export it: `glob()` prefixes `:`
    onto such a result and `exports_files` rejects that as a target name, which
    fails the whole package — every patch in it, not just the scoped one. List
    those files literally:

    ```python
    exports_files(["@acme__diffs@1.3.1.patch", "nanoid@3.3.11.patch"])
    ```

## catalogs, overrides and packageExtensions

These three need no support and have none, because pnpm resolves them at every
use site before writing the lockfile: `catalog:` specifiers, `overrides` (both
plain and the package-scoped `parent>child` form) and `packageExtensions`
already appear as concrete versions and injected peers in the `packages:` and
`snapshots:` sections the extension reads. Tests pin that behaviour so a future
parser change cannot start reading `specifier:` instead of `version:` unnoticed.

## Platform-Specific Packages

A package whose `os`/`cpu` fields exclude your platform — `@rollup/rollup-linux-x64-gnu`
on a Mac, say — is not part of your build, and there is nothing to configure.

Native sidecars still work: a bin script that resolves an optional dependency at
runtime (`oxlint` → `@oxlint/linux-x64-gnu`) gets it in its runfiles, even
though the two are no longer sibling directories inside one repository.

## Bin Scripts

Packages with a `bin` entry in their `package.json` get a `_bin` label:

| npm package | Binary label |
|-------------|-------------|
| `vitest` | `@npm//:vitest_bin` |
| `esbuild` | `@npm//:esbuild_bin` |
| `oxlint` | `@npm//:oxlint_bin` |

Use these as `executable` targets or as `tools` in custom actions. The hub
cannot know whether a package has a bin without downloading it — which is the
cost being avoided — so each `<label>_bin` alias is declared unconditionally and
resolves only when something asks for it. Asking for one on a package with no
bin script is an error at that point, not at load time. The bin chosen is npm's
own convention: the entry named after the package, else the only one.

## npm aliases

A dependency declared under a different name than the package it resolves to —
`"h3-v2": "npm:h3@2.0.1"` — gets its own label, so the name your code imports
exists as a target:

```python
deps = ["@npm//:h3-v2"]
```

An alias label is only created when no real package in the lockfile already
claims that name, and an alias that resolves to two different packages in one
lockfile is an error rather than a coin flip: the hub is one flat namespace.

## node_modules Targets

For test and dev-server targets that need a real `node_modules` directory on
disk:

```python
load("@rules_typescript//npm:defs.bzl", "node_modules")

node_modules(
    name = "node_modules",
    deps = ["@npm//:vitest", "@npm//:react"],
)
```

This builds a `node_modules` tree in the sandbox holding exactly those packages
and their transitive dependencies. `ts_test` does it for you from its `deps` —
see [Testing with vitest](testing.md).

The tree places **every** resolution a closure made, not one per name: a name's
primary resolution keeps the flat top-level directory and any other one gets its
bytes once under `.pnpm/<name>@<version>[_<peer set>]/node_modules/<name>`, with
a relative link from each dependent that resolved to it. A resolution is name,
version and peer set, because pnpm resolves a package once per distinct peer set
and the outcomes have different dependency edges. Declaring two resolutions of
one name directly on one target is an error — see
[node_modules](../rules/node-modules.md#the-layout).

## Why one repository per package

A single repository for the whole lockfile cannot fetch lazily, and not by
oversight: it reads `bin` and `exports` out of each extracted `package.json` to
generate targets, so nothing can be emitted until everything is downloaded. One
serial Starlark loop, no resume, and a target whose only npm dependency is
vitest pays for all of it.

Per-package repositories invert that. The extension does the whole-graph
analysis it can do from the lockfile text alone — platform filtering, which
version a bare label means, `@types` pairing, cycle breaking, alias naming,
patch routing — and then declares one repository per package. Each package reads
its own `package.json` and writes its own BUILD file, so Bazel fetches on
demand, fetches independent repositories in parallel, caches and invalidates per
package, and a malformed tarball fails only its own package.

One measurement, made while both layouts still existed: building one vitest test
target from an empty output base against a real 2731-package lockfile went from
392s and 2.9 GB of `external/` to 66s and 415 MB, fetching 138 packages —
vitest's actual transitive closure — instead of all 2731. The single-repository
implementation has since been deleted, so that comparison cannot be re-run from
this tree; it is recorded here as history, not as a benchmark you can reproduce.

What you *can* reproduce is the shape of it, on your own lockfile, without
building anything:

```bash
bazel query 'kind(ts_npm_package, deps(//path/to:my_test))' | wc -l
```

That counts the package targets the target can reach — very nearly the set of
repositories Bazel would fetch, since a package present under an npm alias name
contributes a second target in the same repository. On this repository's own
lockfile a vitest test target reaches 123 targets in 121 repositories.

There is one npm implementation. The single-repository layout, its
`npm_translate_lock` repository rule and the `npm.translate_lock(lazy = ...)`
attribute are gone: two resolvers is how they drift, and patches could not be
implemented once for both.
