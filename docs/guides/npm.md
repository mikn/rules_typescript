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
use_repo(npm, "npm", "pnpm")
```

`"npm"` is the alias hub your labels spell. `"pnpm"` is not optional either, even
if you never intend to run pnpm through Bazel: the next `bazel run //:gazelle`
writes a `ts_pnpm` and a `ts_add_package` target into your root `BUILD.bazel` —
unconditionally, as soon as a `pnpm-lock.yaml` exists — and both name `@pnpm`.
Without the repo, `bazel build //...` stops before it builds anything:

```
ERROR: no such package '@@[unknown repo 'pnpm' requested from @@ …]//': No
repository visible as '@pnpm' from main repository and referenced by '//:pnpm'
```

Nothing runs those two targets on your behalf, and they cost nothing until you
do — [Hermetic pnpm](#hermetic-pnpm) is what they are for.

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
system install. Gazelle has already written the two macros for you — this is what
the `ts_pnpm` and `ts_add_package` targets in your root `BUILD.bazel` are:

```python
# BUILD.bazel — what `bazel run //:gazelle` writes beside a root pnpm-lock.yaml
load("@rules_typescript//ts:defs.bzl", "ts_add_package", "ts_pnpm")

ts_pnpm(name = "pnpm")

ts_add_package(
    name = "add_package",
    pnpm_lock = "//:pnpm-lock.yaml",
)
```

The `@pnpm` repo they need came from Step 2. To choose the pnpm version rather
than take the default:

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
tree rather than a sandbox. The wrapper is a bash script, so this does not run on
Windows.

### What the wrapper guarantees

Extra `pnpm add` flags are passed through, and the wrapper's whole job is that
the lockfile it rewrites is the hub's. That takes three things, not one, because
pnpm has three routes to the same setting.

**Redirecting the lockfile is refused, in four spellings.** The `--dir` the
target appends overrides a `--dir` of your own, because pnpm takes the last
occurrence. `--lockfile-dir` has nothing to lose to — an appended
`--lockfile-dir` loses to an *earlier* `--lockfile-directory` — so the flag is
rejected instead:

```
--lockfile-dir  --lockfile-directory  --config-lockfile-dir  --config-lockfile-directory
```

Each argument is normalised before the comparison: case-folded, `.` and `_`
turned into `-`, and anything after an `=` stripped. So `--LOCKFILE_DIR=x` and
`--config.lockfile-directory=x` are refused too.

**Everything spelled `NPM_CONFIG_*` or `npm_config_*` is unset.** pnpm reads
settings from the environment as well as from flags, so the flag check alone
would not catch a redirect that arrived that way. The scrub is blunter than its
intent — the `sed` that selects the names matches those two prefixes followed by
anything at all, since every letter of the `lockfile` it looks for in between is
optional, so `NPM_CONFIG_REGISTRY` goes with `NPM_CONFIG_LOCKFILE_DIR` — and it
is also narrower: a mixed-case `Npm_Config_lockfile_dir` survives, and the
after-the-fact check below is what covers that one.

**The outcome is checked afterwards.** The wrapper lists every `pnpm-lock.yaml`
in the tree before and after the run, and any new one outside the hub is
**deleted and the target exits non-zero** — a lockfile no `npm.translate_lock()`
names is a lockfile nothing builds from, and leaving it would shadow the hub's
for anyone running pnpm by hand. That last check is what covers a route this
pattern does not know about.

Two more refusals before pnpm runs at all: no `PNPM_HUB_DIR` (the target was not
generated by `ts_add_package`), and no `package.json` beside the hub's lockfile —
pnpm would create one.

To edit another hub, run that hub's own target.

## More than one hub

A workspace can translate several lockfiles. Each one is its own alias hub, named
by `npm.translate_lock`'s `name` attr, and the name is what `use_repo` takes and
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

Reasons to split rather than to add to one lockfile: a closure that has no
business in the tree an app's tests resolve against (eslint's, say), and a
curated fixture lockfile that no `pnpm add` should regenerate. Against that, two
lockfiles are two things to keep in step and a package resolved in both is
fetched twice. One root lockfile remains the default answer —
[Monorepo Layout](monorepo.md#single-pnpm-lockfile) — and this is the escape
hatch.

Three things follow from a second hub, and each is a place a workspace gets it
wrong:

- **Gazelle has to be told**, per package, which hub that tree's imports come
  from: `# gazelle:ts_npm_hub npm_tools`. Otherwise generated deps name `@npm`,
  which for those packages is a label that does not exist. See
  [More than one npm hub](../gazelle/directives.md#more-than-one-npm-hub).
- **One `ts_add_package` target per hub.** pnpm rewrites whichever lockfile it
  resolves against, so which hub is being edited belongs in the command a person
  types rather than in a default they cannot see:

  ```python
  ts_add_package(
      name = "add_package_tools",
      pnpm_lock = "//tools:pnpm-lock.yaml",
  )
  ```

  `bazel run //:add_package_tools -- eslint` then says out loud what it is about
  to rewrite.
- **A dev-only hub stays out of consumers' lock files.** A hub declared through
  `use_extension(..., dev_dependency = True)` is invisible when your module is
  not the root — which is what you want for a fixture, and not what you want for
  a hub a public rule's action depends on.

## Private and scoped registries

A pnpm lockfile records a package's `name@version` and its integrity and says
nothing about where the bytes came from — there is no registry field anywhere in
it. `.npmrc` is the only record, so a workspace whose packages do not come from
`registry.npmjs.org` cannot be fetched without handing it over:

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

**Credentials are read somewhere else, on purpose.** The extension's result is
serialised into `MODULE.bazel.lock`, which is committed, and a repository rule's
attribute values go into it verbatim — so a token that reached an attribute would
be a token in git. The extension therefore takes the registry lines and nothing
else, and each package's own fetch reads the `.npmrc` again for
`//host/path/:_authToken=` or `:_auth=`. What lands in the lock is the file's
label.

```
//npm.example.com/:_authToken=${NPM_TOKEN}
```

`${VAR}` is expanded from the fetch environment, which is how a token stays out
of the workspace entirely; changing the variable refetches, because reading it
registers it. Credentials are keyed by `//host/path/` with the **longest**
matching prefix winning, so a registry mounted on a path — an Artifactory repo,
say — carries its own token without claiming the whole host.

Two things this deliberately does not do:

- **`~/.npmrc` is not consulted, and cannot be.** It lies outside the workspace,
  so Bazel cannot make it an input, and two machines with different user-level
  files would fetch different bytes from one lockfile and one lock. The part that
  legitimately varies per machine is the token, which `${VAR}` covers.
- **`username` / `_password` fail** with a message naming the file and the scope.
  npm stores `_password` base64-encoded and Starlark has no way to decode it
  (there is no `chr()`). Use `_authToken` — `npm config set //host/:_authToken`
  writes it — or `_auth`, which is the same base64 blob.

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
lockfile `//tests/vitest:math_test` reaches 111 targets in 111 repositories; the
two counts coincide because no alias falls inside that closure.

There is one npm implementation. The single-repository layout, its
`npm_translate_lock` repository rule and the `npm.translate_lock(lazy = ...)`
attribute are gone: two resolvers is how they drift, and patches could not be
implemented once for both.
