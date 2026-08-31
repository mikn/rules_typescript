# npm Dependencies

npm packages come from a `pnpm-lock.yaml`. The `npm` module extension reads that
file, text only and no network, and declares one external repository per package
plus an alias hub named `@npm` that holds nothing but aliases. Bazel fetches a
package's repository the first time something needs it, so a target's npm cost is
its own dependency closure.

## Setup

**Step 1.** Create a `pnpm-lock.yaml`:

```bash
pnpm init
pnpm add react react-dom --lockfile-only
```

`--lockfile-only` updates the lockfile without creating a `node_modules/`
directory. No `node_modules/` exists in the source tree; Bazel materialises one
inside the sandbox for the targets that need it.

A `pnpm-lock.yaml` is the only npm input these rules read — there is no npm or
yarn lockfile path — so this step is not optional. The pnpm that writes it can
be: the extension downloads its own, and `bazel run //:pnpm` runs that one
without a system install. See [Hermetic pnpm](#hermetic-pnpm).

**Step 2.** Add to `MODULE.bazel`:

```python
npm = use_extension("@rules_typescript//npm:extensions.bzl", "npm")
npm.translate_lock(pnpm_lock = "//:pnpm-lock.yaml")
use_repo(npm, "npm", "pnpm")
```

`"npm"` is the alias hub your labels spell. `"pnpm"` is required too: the next
`bazel run //:gazelle` writes `ts_pnpm` and `ts_add_package` targets into your
root `BUILD.bazel` as soon as a `pnpm-lock.yaml` exists, and both name `@pnpm`.
Without the repo, `bazel build //...` stops before it builds anything:

```
ERROR: no such package '@@[unknown repo 'pnpm' requested from @@ (did you mean
'npm'?)]//': The repository '@@[unknown repo 'pnpm' requested from @@ (did you
mean 'npm'?)]' could not be resolved: No repository visible as '@pnpm' from
main repository and referenced by '//:pnpm'
```

Nothing runs those two targets on your behalf. See
[Hermetic pnpm](#hermetic-pnpm).

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

- Scoped packages (`@scope/name`) become `scope_name`: drop the `@`, replace `/`
  with `_`.
- Hyphens are kept as-is.
- A bare label means the **root importer's own resolution** where the lockfile
  gives one, and the highest version otherwise. A package resolved at several
  versions also gets a version-suffixed label per version, so one can be pinned.
- A `workspace:*` link resolves to a target in your own repository. See
  [workspace links](#workspace-links).

## Adding Dependencies

```bash
pnpm add zod --lockfile-only   # updates pnpm-lock.yaml only — no node_modules
bazel run //:gazelle           # Gazelle sees the new import, adds @npm//:zod
bazel build //...              # Bazel fetches just that package's closure
```

`pnpm` is needed only to edit the lockfile. It is not needed at build time, test
time, or on CI.

## Hermetic pnpm

The extension downloads a standalone pnpm binary whether or not you ask for one,
so lockfile edits need no system install. Gazelle has already written the two
macros:

```python
# BUILD.bazel — what `bazel run //:gazelle` writes beside a root pnpm-lock.yaml
load("@rules_typescript//ts:defs.bzl", "ts_add_package", "ts_pnpm")

ts_pnpm(name = "pnpm")

ts_add_package(
    name = "add_package",
    pnpm_lock = "//:pnpm-lock.yaml",
)
```

The `@pnpm` repo they need came from Step 2. `npm.pnpm()` only pins which version
is downloaded; leaving it out gets a default, not nothing:

```python
# MODULE.bazel
npm = use_extension("@rules_typescript//npm:extensions.bzl", "npm")
npm.pnpm(version = "10.32.1")   # optional; a default version is used otherwise
npm.translate_lock(pnpm_lock = "//:pnpm-lock.yaml")
use_repo(npm, "npm", "pnpm")
```

```bash
bazel run //:pnpm -- --version
bazel run //:pnpm -- add zod --lockfile-only
bazel run //:add_package -- zod          # appends --lockfile-only for you
```

`pnpm_lock` is the hub this target edits, spelled exactly as its
`npm.translate_lock()` spells it; pnpm is pointed at that label's directory with
`--dir`. It is required: a `pnpm add` with no hub resolves against the workspace
root and writes a `package.json` and `pnpm-lock.yaml` there. Declaring a label
also makes a missing lockfile, or one this package cannot see, a build error.

A workspace with several hubs gets one target per hub, named after it:

```python
ts_add_package(
    name = "add_package_tailwind",
    pnpm_lock = "//third_party/tailwind:pnpm-lock.yaml",
)
```

Both targets `cd` to `$BUILD_WORKSPACE_DIRECTORY` first, so they edit the source
tree. The wrapper is a bash script, so it does not run on Windows.

### Which Lockfile the Wrapper Edits

Extra `pnpm add` flags are passed through. Three checks keep the rewrite inside
the hub, one per route pnpm has to that setting.

- **Flags.** Four spellings are rejected outright, because an appended
  `--lockfile-dir` would lose to an earlier `--lockfile-directory`:

  ```
  --lockfile-dir  --lockfile-directory  --config-lockfile-dir  --config-lockfile-directory
  ```

  Each argument is normalised first: case-folded, `.` and `_` turned into `-`,
  anything after an `=` stripped. `--LOCKFILE_DIR=x` and
  `--config.lockfile-directory=x` are refused too. `--dir` is allowed, because
  the one the target appends wins: pnpm takes the last occurrence.
- **Environment.** Every variable spelled `NPM_CONFIG_*` or `npm_config_*` is
  unset. The match is broad, so `NPM_CONFIG_REGISTRY` goes along with
  `NPM_CONFIG_LOCKFILE_DIR`; a mixed-case `Npm_Config_lockfile_dir` survives it.
- **Outcome.** The wrapper lists every `pnpm-lock.yaml` in the tree before and
  after the run. Any new one outside the hub is **deleted and the target exits
  non-zero**, which covers the mixed-case environment names and any other route
  to the same setting.

Two more refusals before pnpm runs at all: no `PNPM_HUB_DIR` (the target was not
generated by `ts_add_package`), and no `package.json` beside the hub's lockfile,
which pnpm would create.

To edit another hub, run that hub's own target.

## More than One Hub

A workspace can translate several lockfiles. Each one is its own alias hub, named
by `npm.translate_lock`'s `name` attr, and that name is what `use_repo` takes and
what BUILD labels spell:

```python
# MODULE.bazel
npm = use_extension("@rules_typescript//npm:extensions.bzl", "npm")
npm.translate_lock(pnpm_lock = "//:pnpm-lock.yaml")                       # name defaults to "npm"
npm.translate_lock(name = "npm_tools", pnpm_lock = "//tools:pnpm-lock.yaml")
use_repo(npm, "npm", "npm_tools", "pnpm")
```

```python
deps = ["@npm_tools//:eslint"]
```

Split a lockfile when a closure has no business in the tree an app's tests
resolve against (eslint's, say), or when a lockfile is a curated fixture no
`pnpm add` should regenerate. The cost is two lockfiles to keep in step, and a
package resolved in both is fetched twice. One root lockfile is the default
([Monorepo Layout](monorepo.md#single-pnpm-lockfile)).

Three things follow from a second hub:

- **Gazelle has to be told**, per package, which hub that tree's imports come
  from: `# gazelle:ts_npm_hub npm_tools`. Otherwise generated deps name `@npm`,
  which for those packages is a label that does not exist. See
  [More than one npm hub](../gazelle/directives.md#more-than-one-npm-hub).
- **One `ts_add_package` target per hub.** pnpm rewrites whichever lockfile it
  resolves against, so the hub belongs in the command a person types:

  ```python
  ts_add_package(
      name = "add_package_tools",
      pnpm_lock = "//tools:pnpm-lock.yaml",
  )
  ```

  That hub is then edited with `bazel run //:add_package_tools -- eslint`.

- **A dev-only hub stays out of consumers' lock files.** A hub declared through
  `use_extension(..., dev_dependency = True)` is invisible when your module is
  not the root.

## Private and Scoped Registries

A pnpm lockfile records a package's `name@version` and its integrity, and no
registry anywhere. `.npmrc` is the only record, so a workspace whose packages
come from somewhere other than `registry.npmjs.org` has to pass it:

```python
npm.translate_lock(
    pnpm_lock = "//:pnpm-lock.yaml",
    npmrc = "//:.npmrc",
)
```

Two kinds of line decide a URL, and they are the only lines the extension reads:

```
registry=https://npm.example.com/          # the default for everything
@acme:registry=https://npm.example.com/    # the default for one scope
```

A `tarball:` in the lockfile's `resolution:` is an absolute URL pnpm already
resolved, and it wins over both.

**Credentials are read at fetch time instead.** The extension's result is
serialised into the committed `MODULE.bazel.lock`, so a token that reached an
attribute would be a token in git. Each package's own fetch reads the `.npmrc`
again for `//host/path/:_authToken=` or `:_auth=`; what lands in the lock is the
file's label.

```
//npm.example.com/:_authToken=${NPM_TOKEN}
```

`${VAR}` is expanded from the fetch environment, and changing the variable
refetches, because reading it registers it. Credentials are keyed by
`//host/path/` with the **longest** matching prefix winning, so a registry
mounted on a path (an Artifactory repo, say) carries its own token without
claiming the whole host.

Two limits:

- **`~/.npmrc` is not consulted, and cannot be.** It lies outside the workspace,
  so Bazel cannot make it an input; `${VAR}` covers the part that legitimately
  varies per machine.
- **`username` / `_password` fail** with a message naming the file and the scope.
  npm stores `_password` base64-encoded and Starlark cannot decode it (there is
  no `chr()`). Use `_authToken`, which `npm config set //host/:_authToken`
  writes, or `_auth`, which is the same base64 blob.

## Patched Dependencies

pnpm's `patchedDependencies` used to be ignored silently: the `packages:`
integrity in the lockfile is the byte-identical upstream tarball, so a patched
package was fetched **unpatched**.

Patches are passed as labels: the paths pnpm keeps in `pnpm-workspace.yaml`
cannot be turned into labels by an extension, because a path like
`patches/foo.patch` says nothing about where your Bazel package boundaries fall.

```python
npm.translate_lock(
    pnpm_lock = "//:pnpm-lock.yaml",
    patches = ["//patches:@acme__diffs@1.3.1.patch"],
)
```

Each file is matched to its lockfile entry by filename, pnpm's own convention
from `pnpm patch-commit`: `<name with / replaced by __>@<version>.patch`.

Every pairing is **verified while the extension evaluates**, so a patch nothing
currently depends on is checked too. Four failures, each naming the label:

- **the label resolves to no readable file.** Resolving the label also forces
  the patch's Bazel package to load, so a broken `patches/BUILD.bazel` surfaces
  here.
- **the file's sha256 disagrees with the digest `patchedDependencies` records.**
  pnpm writes that digest when it writes the patch, so a disagreement means the
  patch changed without `pnpm install` being re-run. A pre-pnpm-9 lockfile
  records something other than a sha256; the file still has to be readable, only
  the comparison is skipped.
- **a `patchedDependencies` entry with no matching label.**
- **a passed patch file no entry claims:** the lockfile is stale, or the file is
  misnamed.

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
verification at all**. That is now a hard error, raised **while the extension
evaluates**, so an entry nothing currently depends on is checked too:

```
npm: entries in //:pnpm-lock.yaml whose `resolution:` carries no usable integrity:
  unverified@1.0.0 -> resolution keys: tarball
Bazel would fetch these bytes with nothing to check them against, ...
```

Accepted algorithms are `sha512-`, `sha384-` and `sha256-`. The list is explicit,
so a pre-SRI digest (`sha1-`) is reported here, naming the package, before any
fetch turns it into a checksum error naming a URL.

Three lockfile shapes cannot satisfy it, all dependencies with no published
tarball: a git dependency (`{commit, repo, type: git}`), a
`file:` dependency on a local directory (`{directory, type: directory}`), and a
remote tarball pnpm could not hash (`{tarball}` with no `integrity`). The first
two already failed, with a worse message: with no `tarball:` key the registry URL
is fabricated from the name and the fetch 404s. Depend on such a package as a
workspace member (a `link:` entry, which becomes a target in your own repository;
see [workspace links](#workspace-links)) or vendor its files.

There is **no opt-out**. The check belongs at extension evaluation, and a module
extension cannot read build flags.

## catalogs, overrides and packageExtensions

pnpm resolves all three at every use site before writing the lockfile, so they
need no support and have none. `catalog:` specifiers, `overrides` (both plain and
the package-scoped `parent>child` form) and `packageExtensions` already appear as
concrete versions and injected peers in the `packages:` and `snapshots:` sections
the extension reads.

## Platform-Specific Packages

A package whose `os`/`cpu` fields exclude your platform — `@rollup/rollup-linux-x64-gnu`
on a Mac, say — is not part of your build, and there is nothing to configure.

Native sidecars still work: a bin script that resolves an optional dependency at
runtime (`oxlint` → `@oxlint/linux-x64-gnu`) gets it in its runfiles, even though
the two are no longer sibling directories inside one repository.

## Where a package's type declarations come from

Each package target carries one declaration entry point: the file the package's
own metadata designates, read in the order a resolver reads it. That entry is
what the `ts_compile` boundary type-checks against and what the
[IDE tsconfig](../getting-started/ide-setup.md) puts in `compilerOptions.paths`.

1. **`exports`, in the map's own key order.** Node and TypeScript try conditions
   as they are written, so a package that writes `require` before `import` means
   that. The walk descends `types`, `typings`, `node`, `import`, `require` and
   `default`, follows array fallbacks, and understands the conditions-only
   shorthand (a map with no `.`-prefixed keys is itself the root entry) and a
   plain string. A leaf naming `.js`, `.mjs` or `.cjs` resolves to the
   declaration beside it: `./dist/node/index.js` → `./dist/node/index.d.ts`.
2. **Top-level `types`, then `typings`,** including the extensionless form
   (`"typings": "dist/index"` → `dist/index.d.ts`). This is where a package with
   no `exports` publishes its declarations, and where **every `@types/*` package**
   publishes them.

Every candidate is checked against the extracted package before it is used, so a
manifest naming a `.d.ts` it does not actually ship falls through to the next
candidate. Six `@babel/helper-*` resolutions in this repository's own lockfile
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

Use these as `executable` targets or as `tools` in custom actions. The hub cannot
know whether a package has a bin without downloading it, so each `<label>_bin`
alias is declared unconditionally and resolves only when something asks for it.
Asking for one on a package with no bin script is an error at that point, not at
load time. The bin chosen follows npm's own convention: the entry named after the
package, else the only one.

## npm Aliases

A dependency declared under a different name than the package it resolves to,
`"h3-v2": "npm:h3@2.0.1"`, gets its own label, so the name your code imports
exists as a target:

```python
deps = ["@npm//:h3-v2"]
```

An alias label is created only when no real package in the lockfile already
claims that name. An alias that resolves to two different packages in one
lockfile is an error, because the hub is one flat namespace.

## Workspace Links

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
itself. It is a generated rule and not an `alias`: Bazel resolves an alias before
any rule implementation runs, so `ts_compile` would see no record of the name.

The target a `link:` entry points at is **looked up, not derived**. A member is a
directory, and which directory inside it holds the target that compiles it is a
Gazelle decision: the default boundary mode gives every directory holding sources
its own package, `# gazelle:ts_package_boundary tsconfig` rolls the subtree up
into the directory holding `tsconfig.json`, and `# gazelle:ts_target_name`
renames the result. So the hub walks from the directories the member's own
manifest designates an entry point in up to the member's root, and takes the
innermost one that declares a target of that name:

```text
link:packages/shared, main: src/index.ts
  packages/shared/src/BUILD.bazel declares :src     →  //packages/shared/src:src
  only packages/shared/BUILD.bazel declares :shared →  //packages/shared:shared
```

The entry points come from `main`, `module` and `exports["."]` -- all three, so a
member that declares only an exports map (`{".": "./src/index.ts"}`, or a
condition map under it) is walked from `src/` as well. A condition outside
`types`/`typings`/`node`/`import`/`require`/`default`, and a target holding a
`*`, are not followed.

That target has to be visible to the hub repository, so
`visibility = ["//visibility:public"]`. A `ts_compile` gets the npm name
attached. A member with no declarations, such as a `css_module` or an
`asset_library`, is forwarded as it is and carries no name.

!!! warning "A member whose target is not declared gets no hub target"
    If no candidate directory declares a target of the member's name, the hub
    declares nothing for that name and writes a comment saying so where the
    label would have been. `@npm//:<member>` then fails as an undeclared target
    for whatever asks for it. That covers a member with no `BUILD.bazel` at all
    **and** one whose `BUILD.bazel` declares something else -- a lone
    `ts_config`, say. Neither earns a label: a label naming a target Bazel
    cannot resolve fails analysis for everything that reaches the hub, not just
    for the member. Run Gazelle, or write the member's target by hand.

    The lookup covers the member's own subtree only. A boundary that rolls a
    member up into a directory **above** it -- a `tsconfig.json` at
    `packages/` rather than at `packages/shared/` -- leaves the member with no
    target of its own, and the hub reports it the same way.

A workspace member is staged into `node_modules` like any other package, so a
`ts_test` or `ts_binary` that lists `@npm//:shared` can import it at run time and
not only type-check against it. Its own npm dependencies come along; a generated
`package.json` marks it ESM. Node falls back to `index.js` at the package root,
so a member whose entry point is named something else is resolvable in the
compiler and not in Node.

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
and their transitive dependencies. `ts_test` does it for you from its `deps`. See
[Testing with vitest](testing.md).

The tree places **every** resolution a closure made, not one per name. A name's
primary resolution keeps the flat top-level directory; any other one gets its
bytes once under `.pnpm/<name>@<version>[_<peer set>]/node_modules/<name>`, with
a relative link from each dependent that resolved to it. A resolution is name,
version and peer set: pnpm resolves a package once per distinct peer set, and
those outcomes have different dependency edges. Declaring two resolutions of
one name directly on one target is an error. See
[node_modules](../rules/node-modules.md#the-layout).

## One Repository per Package

A single repository for the whole lockfile cannot fetch lazily: it reads `bin`
and `exports` out of each extracted `package.json` to generate targets, so
nothing can be emitted until everything is downloaded.

The extension instead does the whole-graph analysis the lockfile text alone
supports — platform filtering, which version a bare label means, `@types`
pairing, cycle breaking, alias naming, patch routing — and declares one
repository per package. Each package reads its own `package.json` and writes its
own BUILD file, so Bazel fetches on demand, fetches independent repositories in
parallel, caches and invalidates per package, and a malformed tarball fails only
its own package.

One measurement, made while both layouts still existed: building one vitest test
target from an empty output base against a real 2731-package lockfile went from
392s and 2.9 GB of `external/` to 66s and 415 MB, fetching 138 packages —
vitest's transitive closure — out of 2731. The single-repository implementation
has since been deleted, so the comparison cannot be re-run from this tree.

To count the package targets one target reaches, without building anything:

```bash
bazel query 'kind(ts_npm_package, deps(//path/to:my_test))' | wc -l
```

That is very nearly the set of repositories Bazel would fetch: a package present
under an npm alias name contributes a second target in the same repository. On
this repository's own lockfile `//tests/vitest:math_test` reaches 113 targets in
113 repositories.

The single-repository layout, its `npm_translate_lock` repository rule and the
`npm.translate_lock(lazy = ...)` attribute are gone.
