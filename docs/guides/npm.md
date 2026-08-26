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
  downloaded package — see [workspace links](#workspace-links).

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

ts_add_package(
    name = "add_package",
    pnpm_lock = "//:pnpm-lock.yaml",
)
```

```bash
bazel run //:pnpm -- --version
bazel run //:pnpm -- add zod --lockfile-only
bazel run //:add_package -- zod          # appends --lockfile-only for you
```

`pnpm_lock` is the hub this target edits, spelled exactly as its
`npm.translate_lock()` spells it, and pnpm is pointed at that label's directory
with `--dir`. It is required: a `pnpm add` with no hub resolves against the
workspace root and writes a `package.json` and `pnpm-lock.yaml` there, which is
a hub nothing translated. Declaring the label rather than a bare path is what
makes a lockfile that does not exist, or one this package cannot see, a build
error instead of a stray file in your tree.

A workspace with several hubs gets one target per hub, named after it, so the
command a person types says which lockfile is about to be rewritten:

```python
ts_add_package(
    name = "add_package_tailwind",
    pnpm_lock = "//third_party/tailwind:pnpm-lock.yaml",
)
```

Both targets `cd` to `$BUILD_WORKSPACE_DIRECTORY` first, so they edit the source
tree rather than a sandbox. The wrapper is a bash script and the binary is
published for Linux and macOS only, so this does not work on Windows.

Extra `pnpm add` flags are passed through, with one refusal: `--lockfile-dir` is
rejected. The `--dir` the target appends overrides a `--dir` of your own, because
pnpm takes the last occurrence, but `--lockfile-dir` is a different flag with
nothing to lose to — so it would write a `pnpm-lock.yaml` outside the hub, which
is the stray lockfile these targets exist to prevent. To edit another hub, run
that hub's own target.

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

## Integrity

Every `packages:` entry has to carry an integrity the download can be checked
against. A package whose `resolution:` has none used to be fetched with **no
verification at all** — no warning, no failure, nothing distinguishing a
registry that answered with the published bytes from one that answered with
something else.

That is now a hard error, raised **when the extension runs** rather than when the
package enters a build's closure, so an entry nothing currently depends on is
checked too:

```
npm: entries in //:pnpm-lock.yaml whose `resolution:` carries no usable integrity:
  unverified@1.0.0 -> resolution keys: tarball
Bazel would fetch these bytes with nothing to check them against, ...
```

Accepted algorithms are `sha512-`, `sha384-` and `sha256-`. The list is explicit
so that a pre-SRI digest (`sha1-`) is reported here, naming the package, instead
of surfacing as a checksum error naming a URL.

Three lockfile shapes cannot satisfy this, all of them dependencies with no
published tarball behind them: a git dependency (`{commit, repo, type: git}`), a
`file:` dependency on a local directory (`{directory, type: directory}`), and a
remote tarball pnpm could not hash (`{tarball}` with no `integrity`). The first
two already failed, just worse — with no `tarball:` key the registry URL is
fabricated from the name and the fetch 404s. Depend on such a package as a
workspace member instead (a `link:` entry, which becomes a target in your own
repository — see [workspace links](#workspace-links)) or vendor its files.

There is **no opt-out**, by the same reasoning as the patch checks above and
`ts_compile`'s strict deps: a flag that turns verification off is a flag that
ends up in someone's `.bazelrc`. There is also nowhere to put one — the check
belongs at extension evaluation, and a module extension cannot read build flags.

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

## Where a package's type declarations come from

Each package target carries one declaration entry point, and it is the file the
package's own metadata designates — read in the order a resolver reads it, not in
an order this ruleset prefers. That entry is what the `ts_compile` boundary
type-checks against and what the [IDE tsconfig](../getting-started/ide-setup.md)
puts in `compilerOptions.paths`.

1. **`exports`, in the map's own key order.** Node and TypeScript try conditions
   as they are written, so a package that writes `require` before `import` means
   that; a fixed priority list would answer with the wrong build's declarations.
   The walk descends `types`, `typings`, `node`, `import`, `require` and
   `default`, follows array fallbacks, and understands the conditions-only
   shorthand (a map with no `.`-prefixed keys *is* the root entry) and a plain
   string. A leaf naming `.js`, `.mjs` or `.cjs` resolves to the declaration
   beside it — `./dist/node/index.js` → `./dist/node/index.d.ts`.
2. **Top-level `types`, then `typings`,** including the extensionless form
   (`"typings": "dist/index"` → `dist/index.d.ts`). This is where a package with
   no `exports` publishes its declarations, and where **every `@types/*` package**
   publishes them.

Every candidate is checked against the extracted package before it is used, so a
manifest naming a `.d.ts` it does not actually ship falls through to the next
candidate rather than producing a target with a missing source. That is not a
hypothetical: six `@babel/helper-*` resolutions in this repository's own lockfile
designate a `lib/index.d.ts` their tarball does not contain.

!!! note "`paths` for subpaths is rooted at the entry's directory"
    A package designating `dist/index.d.ts` gets `pkg/*` → `dist/*`. Importing
    `pkg/sub` where the subpath's declarations sit somewhere other than beside the
    entry will not resolve in the editor, even though the build is fine.

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

## workspace links

A `workspace:*` dependency resolves to a target in your own repository, and its
hub label carries the npm name the lockfile imports it under:

```yaml
# pnpm-lock.yaml
    dependencies:
      shared:
        specifier: workspace:*
        version: link:packages/shared
```

```python
deps = ["@npm//:shared"]     # → //packages/shared:shared, importable as "shared"
```

`import { x } from "shared"` resolves because that hub target declares the name
itself. It is a generated rule rather than an `alias` for exactly that reason:
Bazel resolves an alias before any rule implementation runs, so the aliased
target arrives at `ts_compile` with no record of the name, and every workspace
member used to have to restate its own npm name as `module_name` — a second
place for it to be wrong, in a file the lockfile never reaches.

The label a `link:` entry points at is `//<link path>:<last path segment>`:
`link:packages/shared` means `//packages/shared:shared`. That target has to exist
and produce declarations (a `ts_compile`), and it has to be visible to the hub
repository, so `visibility = ["//visibility:public"]`.

!!! note "The IDE tsconfig still reads `module_name`"
    The checked-in tsconfig is generated by an aspect over your `ts_compile`
    graph, and it takes a bare specifier from the `module_name` attribute. A
    member with none builds fine and resolves in Bazel, but its bare import will
    not resolve in the editor. Set `module_name` there too if that matters.

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
lockfile a vitest test target reaches 74 targets in 74 repositories; the two
counts coincide because no alias falls inside that closure.

There is one npm implementation. The single-repository layout, its
`npm_translate_lock` repository rule and the `npm.translate_lock(lazy = ...)`
attribute are gone: two resolvers is how they drift, and patches could not be
implemented once for both.
