# Troubleshooting

## No repository visible as '@rules_rust'

```
ERROR: @rules_rust//rust/toolchain/channel :: Error loading option
@rules_rust//rust/toolchain/channel: No repository visible as '@rules_rust'
from main repository
```

A `.bazelrc` in your workspace sets a `--@rules_rust//...` flag. `rules_rust` is
a transitive dependency of `rules_typescript`, so `@rules_rust` is not in your
module's repo mapping and Bazel cannot resolve the flag's label. Delete the line:
the channel defaults to `stable`, the only channel `rules_typescript` registers a
toolchain for.

For Rust code of your own, `bazel_dep(name = "rules_rust", version = "0.73.0")`
brings the repo into your mapping. The flag then applies to the `oxc-bazel` build
too, where a non-`stable` channel fails toolchain resolution.

## No repository visible as '@pnpm'

```
ERROR: no such package '@@[unknown repo 'pnpm' requested from @@ (did you mean
'npm'?)]//': The repository '@@[unknown repo 'pnpm' requested from @@ (did you
mean 'npm'?)]' could not be resolved: No repository visible as '@pnpm' from
main repository and referenced by '//:pnpm'
```

The root `BUILD.bazel` holds a `ts_pnpm(name = "pnpm")` or a
`ts_add_package`, and both name `@pnpm`. Take the repo:

```python
npm = use_extension("@rules_typescript//npm:extensions.bzl", "npm")
npm.translate_lock(pnpm_lock = "//:pnpm-lock.yaml")
use_repo(npm, "npm", "pnpm")
```

`npm.pnpm(version = …)` is optional; a default version is used. See
[npm Dependencies § Setup](npm.md#setup).

## No repository visible as '@npm'

```
ERROR: no such package '@@[unknown repo 'npm' requested from @@]//': The
repository '@@[unknown repo 'npm' requested from @@]' could not be resolved:
No repository visible as '@npm' from main repository and referenced by
'//src/lib:lib_test'
```

Gazelle resolved a bare import to an `@npm//:…` label and `use_repo` does not
take `"npm"`. One npm dependency is enough to need the extension: the three
lines above. A label naming a second hub (`@npm_tools//:…`) means that hub is
missing from `use_repo`; see [more than one hub](npm.md#more-than-one-hub).

## No test targets were found, yet testing was requested

```
INFO: Found 2 targets and 0 test targets...
ERROR: No test targets were found, yet testing was requested
```

`bazel test //...` exits 4 when the pattern matches no test target, and a project
with no `*.test.ts` / `*.spec.ts` yet has none. Gazelle generates a `ts_test` from
the first such file it sees; vitest comes from your lockfile.
[The quickstart's step 8](../getting-started/quickstart.md#path-a-new-project)
walks the first-test path.

## BUILD file not found for //:MODULE.bazel

```
Error in path: Unable to load package for //:MODULE.bazel: BUILD file not
found in any of the following directories.
```

`rules_rust`'s crate fetching resolves `//:MODULE.bazel`, which requires your
repository root to be a Bazel package. Create a `BUILD.bazel` at the root; an
empty file is enough, though the [quickstart](../getting-started/quickstart.md)
puts the Gazelle target there.

## No tsgo Toolchain / Declarations Are Missing

Toolchain registration is the consumer's job. Your `MODULE.bazel` needs:

```python
register_toolchains("@rules_typescript//ts/toolchain:all")
```

Without it nothing resolves the tsgo toolchain, and under the default
`--//ts:declarations=tsgo` that means no `.d.ts` and no type-checking.

To choose the compiler, point the `ts` extension at the lockfile that pins
`typescript` (7 or later):

```python
ts = use_extension("@rules_typescript//ts:extensions.bzl", "ts")
ts.tsgo(pnpm_lock = "//:pnpm-lock.yaml")
```

`ts.tsgo(version = "7.0.2")` downloads a named release unverified instead. Both
are in [Version Pinning](../getting-started/quickstart.md#version-pinning).

Windows is not supported, so no tsgo toolchain resolves there. See
[COMPATIBILITY.md](https://github.com/mikn/rules_typescript/blob/main/COMPATIBILITY.md#windows).

## npm: pnpm-lock.yaml declares patchedDependencies with no patch file

Your lockfile patches a package and the extension was not given the patch. Pass
it as a label:

```python
npm.translate_lock(
    pnpm_lock = "//:pnpm-lock.yaml",
    patches = ["//patches:@acme__diffs@1.3.1.patch"],
)
```

A patch file that no `patchedDependencies` entry claims fails too, and means the
lockfile is stale or the file is misnamed. Names follow `pnpm patch-commit`:
`<name with / replaced by __>@<version>.patch`. See
[npm Dependencies](npm.md#patched-dependencies).

## npm: patch labels that resolve to no readable file

```
npm: patch labels passed to npm.translate_lock(patches = [...]) that resolve to
no readable file:
  @@//patches:@acme__diffs@1.3.1.patch
```

The label is wrong, the file is missing, or the Bazel package holding it failed
to load. A patch filename starting with `@` causes the last: `glob()` prefixes
`:` onto the result and `exports_files` rejects it as a target name, failing the
whole package and every patch in it. List those files literally:

```python
exports_files(["@acme__diffs@1.3.1.patch", "nanoid@3.3.11.patch"])
```

## npm: patch files whose sha256 disagrees with the lockfile

```
npm: patch files whose sha256 disagrees with the one pnpm-lock.yaml records in
patchedDependencies:
  @@//patches:@acme__diffs@1.3.1.patch
    lockfile: 384aa81a…
    file:     19bbd346…
```

pnpm writes that digest when it writes the patch, so a disagreement means the
patch file changed without `pnpm install` being re-run. Re-run it (`pnpm install
--lockfile-only`) so the lockfile records what the file now is, or restore the
file. Bazel does not apply a patch pnpm never saw.

## node_modules: depends on two versions of one name at once

```
node_modules: @@//src/app:node_modules depends on two versions of 'minimatch' at once:
  minimatch@10.2.4
  minimatch@9.0.9
```

`node_modules/<name>` is one directory, so no arrangement of the tree answers
`import "<name>"` with both. Depend on one version here and let the other arrive
through the package that needs it, where it keeps its own store directory, or
split the two into separate `node_modules` targets. See
[node_modules](../rules/node-modules.md#two-resolutions-of-one-name-in-deps).

A narrower form covers two resolutions of one version that differ in the peers
pnpm resolved them against:

```
node_modules: @@//src/app:node_modules depends on two resolutions of
'fdir@6.5.0' at once, one per peer set:
  peers picomatch_4_0_3_<digest>
  peers picomatch_4_0_7_<digest>
```

The tarball is the same both ways; what differs is what the package's own
dependencies resolve to, and that lives in the one
`node_modules/<name>/node_modules`.

## imports files no direct dep provides

```
ERROR: .../src/app/BUILD.bazel:3:11: TsgoDeclare //src/app:app failed: (Exit 1)
tsaction: //src/app:app imports files no direct dep provides:
  src/app/main.ts imports "zod"
    resolved to node_modules/zod/index.d.ts
    add @npm//:zod to deps
```

The import resolves today only because it reaches this target through another
dep's own deps, and stops resolving the moment that dep drops it. Add the label
the message names, or run Gazelle:

```bash
bazel run //:gazelle
```

Gazelle writes `deps` from tsgo's own listing of the program, the listing the
check reads, so a Gazelle run that leaves a reported failure unfixed is a bug.
Outside the check: an edge from a dep's own file, which is that dep's to
declare; a tsconfig `types` entry; and an import nothing in the closure
provides, which TypeScript reports as `TS2307`.

## Option 'baseUrl' has been removed

```
pkg/tsconfig.json(3,5): error TS5102: Option 'baseUrl' has been removed.
Please remove it from your configuration.
```

`baseUrl` is gone in tsgo, and the generated config cannot take it back out of
your file: the diagnostic fires on the key being present anywhere in the
`extends` chain, so re-stating it above yours only moves the error. Delete it
from the `tsconfig.json` the target names. `paths` here is Bazel's, rewritten
per configuration, so nothing in this ruleset resolves against a `baseUrl`.

## Option 'moduleResolution' must be set to 'NodeNext'

```
pkg/tsconfig.json(2,3): error TS5109: Option 'moduleResolution' must be set to
'NodeNext' (or left unspecified) when option 'module' is set to 'NodeNext'.
```

Two files each supply one half of a coupled pair. The ruleset's baseline states
no `moduleResolution`, so a `module` of yours never has a stray baseline
resolver under it. Your own `extends` chain still can: a `module` in one file
and a `moduleResolution` in another. Put both halves in one file, or drop the
`moduleResolution` and let tsgo derive it.

The mirror image, `TS5110`, is a `moduleResolution` of `Node16`/`NodeNext` with
no `module` beside it.

## Import Not Resolving in tsgo

tsgo resolves with `moduleResolution: "Bundler"` (what tsgo derives from every
`module` but `Node16`/`NodeNext`) against the `node_modules` forest built from
`deps`. A bare import that resolves nowhere, with no strict-deps failure and
only `TS2307`, means no dep in the closure provides it. Add the package:

```python
ts_compile(
    name = "app",
    srcs = ["app.ts"],
    deps = ["@npm//:zod"],
)
```

## Cannot find type definition file for 'vite/client'

```
app.ts(1,23): error TS2688: Cannot find type definition file for 'vite/client'.
```

From a `/// <reference types="vite/client" />`, the line Vite's own project
template puts at the top of `src/vite-env.d.ts`. The directive resolves through
TypeScript's type-reference resolver, which walks `node_modules/@types` and the
`node_modules` above the file; under Bazel that tree is the forest built from
`deps`, so the package has to be there:

```python
ts_compile(
    name = "app",
    srcs = ["src/main.ts", "src/vite-env.d.ts"],
    deps = ["@npm//:vite"],
)
```

Gazelle writes that dep from the listing: the directive is an edge from
`vite-env.d.ts` to the package's file, resolved by tsgo, and the package it
landed in is the label
([Import Resolution](../gazelle/overview.md#import-resolution)). A whole tree
that needs a package names it in the `types` of the tsconfig its packages
extend, which is an edge of every program in the tree.

That is the directive in a file of your own: Gazelle writes the dep it names,
and the tsgo check fails the build while it is missing
([Deps Have to Be Direct](../rules/ts-compile.md#deps-have-to-be-direct)). One
in an npm package's declaration entry (`@types/bun/index.d.ts` is
`/// <reference types="bun-types" />`) is followed by the rule; see
[`@types/*` packages](../rules/ts-compile.md#types-packages).

## ts_test: needs vitest in the node_modules tree, which no dep provides

```
ts_test @@//path/to:my_test: @@//ts/runners:vitest runs the tests and needs
vitest in the node_modules tree, which no dep provides.
Add the hub label of each to deps.
```

The vitest runner runs `vitest` out of the tree the tests run in, the forest
the test's `deps` build, and a package no dep provides is not in it. List
`@npm//:vitest` in `deps`:

```python
ts_test(
    name = "my_test",
    srcs = ["my.test.ts"],
    deps = [":my_lib", "@npm//:vitest"],
)
```

Every other package the run needs at runtime is in the tree the same way: the
test's npm deps, their closures, and each `ts_compile` dep's npm closure, so a
package only the production code imports arrives through that dep. See
[Runners](../rules/ts-test.md#runners) and
[Listing npm Deps](../rules/ts-test.md#listing-npm-deps).

## Isolated Declarations Error: Missing Return Type

Reachable only under `--//ts:declarations=oxc`, where Oxc derives `.d.ts` from
syntax and so needs an explicit type on every export:

```
× Isolated declarations error(s): TS9013: Expression type can't be inferred
│ with --isolatedDeclarations.
```

Add the annotation Oxc names, or build under the default
`--//ts:declarations=tsgo`, where the compiler infers it. See
[Isolated Declarations](../getting-started/isolated-declarations.md).

## Type Errors Are Not Failing the Build

Under the default `--//ts:declarations=tsgo` they always do: the `.d.ts` are
outputs of the type-checking action, so a target with a type error produces
nothing.

Under `--//ts:declarations=oxc`, type-checking moves into the `_validation` output
group, off the critical path. Bazel runs those actions during `bazel build` on
its own, unless `--norun_validations` turns them off; the `.bazelrc` line the
quickstart writes requests the group explicitly:

```
build --output_groups=+_validation
```

## Gazelle Generating Wrong Deps

`deps` is what tsgo resolved when it listed the package's `tsconfig.json`, so
a wrong dep is a wrong resolution or a wrong label for the right file:

1. Run `bazel run //:gazelle -- -ts_verbose` and read the program's line: the
   files listed, tsgo's diagnostics, and the imports it could not resolve.
2. A missing npm dep is an import the checkout's `node_modules` does not
   answer (`pnpm install`), or a name the root `pnpm-lock.yaml` never mentions;
   the run names the importer and the specifier it refused.
3. A missing first-party dep is a file no package owns: the nearest
   `tsconfig.json` above it does not list it, or its directory is under a
   `# gazelle:exclude`; the run names the file and the programs that reached
   it. Put it in a program, or name its label with `# gazelle:resolve`.
4. `# keep` above a rule, or on a `deps` entry, holds what you wrote
   ([`# keep`](../gazelle/directives.md#keep)).

## ts_dev_server: has no node_modules attr

```
ts_dev_server: @@//src/app:dev has no node_modules attr, so the app's own
dependencies are not in runfiles.
Add node_modules = ":node_modules" pointing at a node_modules() target; the
generated config resolves every bare specifier through that tree.
```

Nothing in the source tree says which tree the app resolves against, and
Gazelle does not write or touch `ts_dev_server`. Declare the tree in the dev
server's own Bazel package, listing every npm package the app imports, plus
`@npm//:vite` under the default server, and name it on the rule:

```python
node_modules(
    name = "node_modules",
    deps = ["@npm//:vite", "@npm//:zod"],
)
```

Name it `node_modules`; see the next entry.

## Cannot find package 'rolldown' imported from …/vite/dist/node/chunks/node.js

```
Error [ERR_MODULE_NOT_FOUND]: Cannot find package 'rolldown' imported from
…/bin/src/app/dev_node_modules/vite/dist/node/chunks/node.js
Did you mean to import "file:///…/dev_node_modules/rolldown/dist/parse-ast-index.mjs"?
```

The `node_modules()` target is named something other than `node_modules`. The
tree is materialised at the target's own name, and Node resolves a bare specifier
by walking up for a directory called `node_modules` and nothing else, so Vite 8,
whose entry imports `rolldown` that way, does not start. The "Did you mean" line
shows the package in the tree.

Rename the target to `node_modules` (one per Bazel package) and update the attr
pointing at it. `@vitejs/plugin-react` fails the same walk-up for its
`react-refresh` runtime, so `react_refresh = True` needs the same name.

## ts_dev_server: sets react_refresh = True, but @vitejs/plugin-react did not load

The dev server does not start: it could not load the Fast Refresh plugin out of
the Bazel `node_modules` tree, and the message ends with the underlying cause.
Usually the package is not in the tree; add it to the `node_modules` target the
dev server uses:

```python
node_modules(
    name = "node_modules",
    deps = [
        "@npm//:vite",
        "@npm//:vitejs_plugin-react",
    ],
)
```

The target name matters too: the plugin resolves the `react-refresh` runtime by
Node's own walk-up, which only looks in directories called `node_modules`.

## [rules_typescript] Failed to load vite_config

`ts_dev_server` loads a copy of your `vite_config` from
`bazel-bin`, so the file's own imports resolve beside the Bazel npm tree, not in
your source tree. Staged there are the config and the modules `vite_config_srcs`
declares, nothing else:

- a relative import of a module not in `vite_config_srcs` fails, and the
  message names the file. Declare it:

  ```python
  vite_config = "vite.plugins.ts",
  vite_config_srcs = glob(["plugins/**/*.ts"]),
  ```

  A module outside the config's own Bazel package cannot be declared, since it
  would have to stage above the staging root; that is a separate analysis-time
  error naming the file and the package;
- a bare npm import works, as long as the `node_modules` target is in the same
  Bazel package as the dev server. Move it back beside the server, or add a
  `node_modules` target there.

See [`vite_config`: what it may import](dev-server.md#vite_config-what-it-may-import).

## [rules_typescript] ts_dev_server: the vite_config sets …, which the generated config does not read

The generated config reads `plugins` out of your `vite_config` and nothing else;
the dev server takes its serve root from the target. Every other key would be
silently discarded, so the load throws, naming the keys it found and the keys it
honours. A framework config that sets `define`, `resolve.alias`, `build.target`,
`optimizeDeps` or `root` hits this. Move what you need into a plugin. The check
runs at config-load time, not analysis time, because only the loaded object says
what keys it has.

## Dev server: Failed to resolve import "some-package"

The dev server resolves bare specifiers through the `node_modules` tree only, and
the package is not in that tree. Add it to the target's `deps` and restart:

```python
node_modules(
    name = "node_modules",
    deps = ["@npm//:vite", "@npm//:some-package"],
)
```

## invalid repository name '{$username}.tsx'

A source file whose name starts with `@` (a TanStack Start route on a dynamic
segment is written `@{$username}.tsx`) reached a `srcs` list bare. A `srcs`
entry is a label, and Bazel parses the head of the string before it looks at the
file system: `@` opens a repository name, `//` an absolute package, and a `:`
anywhere splits package from target. The bare name names a repository that does
not exist, and one such entry fails `bazel query //...` for every package in the
workspace, not for the one target.

Gazelle writes `":@{$username}.tsx"` for these, which pins the name to the
package the way a bare name does for every other file. Hand-written BUILD files
need the same leading colon.

A name holding a `:` has no label in any spelling, since a target name may not
contain one. Gazelle leaves such a file out of every target it generates, saying
so on one line per file. Rename the file.

`exports_files` takes the opposite spelling. Its argument is a list of target
names, not of labels, so the bare `exports_files(["@{$username}.tsx"])` is the
form there, and `exports_files([":@{$username}.tsx"])` fails with
`target names may not contain ':'`. A `filegroup` whose `srcs` is a `glob()`
needs neither spelling: a glob pattern is matched against the file system, not
parsed as a label.

## Snapshot 'x 1' mismatched, or a snapshot vitest says is new

`ts_test` runs vitest in read-only snapshot mode, so a mismatch is a failure and
a snapshot the sandbox cannot read counts as absent. Two causes:

- The `.snap` is not in `srcs`, so it never reached the runfiles tree. List it
  with the package's other files, as Gazelle does.
- The snapshot is stale. Regenerate it with `vitest -u` in the package, then
  commit.

Full workflow: [Snapshots](testing.md#snapshots).

## Slow First Build

The first build downloads a Rust toolchain, a tsgo npm tarball, a Node.js
tarball, and the npm packages your targets reach (not the whole lockfile), then
compiles `oxc-bazel` and its crate graph from Rust source. That compile takes
minutes. Everything after it is cached; do not `bazel clean`.

Mount a persistent cache volume so CI pays it once:

```bash
docker run -v bazel-cache:/root/.cache/bazel my-image bazel build //...
```

## Container Builds

Bazel works inside Docker containers without privileged mode:

```dockerfile
FROM ubuntu:24.04

RUN apt-get update && apt-get install -y curl git \
    && rm -rf /var/lib/apt/lists/*

RUN curl -Lo /usr/local/bin/bazel \
    https://github.com/bazelbuild/bazelisk/releases/latest/download/bazelisk-linux-amd64 \
    && chmod +x /usr/local/bin/bazel

WORKDIR /workspace
COPY . .
RUN bazel build //...
```

Mount a cache volume, for the reason above. ARM64 containers work: `rules_rust`
builds `oxc-bazel` natively and the toolchain fetches the compiler's
`linux-arm64` platform package (`@typescript/typescript-linux-arm64` for a
`typescript` release).
