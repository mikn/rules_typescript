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

Gazelle writes `ts_pnpm(name = "pnpm")` and `ts_add_package(name =
"add_package")` into your root `BUILD.bazel` as soon as a root `pnpm-lock.yaml`
exists, whether or not you use the hermetic-pnpm workflow, and both name
`@pnpm`. Take the repo:

```python
npm = use_extension("@rules_typescript//npm:extensions.bzl", "npm")
npm.translate_lock(pnpm_lock = "//:pnpm-lock.yaml")
use_repo(npm, "npm", "pnpm")
```

`npm.pnpm(version = …)` is optional; a default version is used. Nothing runs
those two targets on your behalf. See [npm Dependencies § Setup](npm.md#setup).

## No repository visible as '@npm'

```
ERROR: no such package '@@[unknown repo 'npm' requested from @@]//': The
repository '@@[unknown repo 'npm' requested from @@]' could not be resolved:
No repository visible as '@npm' from main repository and referenced by
'//src/lib:_lib_test_node_modules'
```

Gazelle resolved a bare import to an `@npm//:…` label and there is no hub yet.
One npm dependency is enough to need the extension — the three lines above. A
label naming a second hub (`@npm_tools//:…`) means that hub is missing from
`use_repo`; see [more than one hub](npm.md#more-than-one-hub).

## No test targets were found, yet testing was requested

```
INFO: Found 2 targets and 0 test targets...
ERROR: No test targets were found, yet testing was requested
```

`bazel test //...` exits 4 when the pattern matches no test target, and a project
with no `*.test.ts` / `*.spec.ts` yet has none. Gazelle generates a `ts_test` from
the first such file it sees; vitest comes from your lockfile.
[The quickstart's step 9](../getting-started/quickstart.md#path-a-new-project)
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

## No tsgo toolchain / declarations are missing

Toolchain registration is the **consumer's** job. Your `MODULE.bazel` needs:

```python
register_toolchains("@rules_typescript//ts/toolchain:all")
```

Without it nothing resolves the tsgo toolchain, and under the default
`declarations = "tsgo"` that means no `.d.ts` and no type-checking.

To pin a tsgo version:

```python
ts = use_extension("@rules_typescript//ts:extensions.bzl", "ts")
ts.tsgo(version = "7.0.0-dev.20260311.1")
```

Windows is not supported, so no tsgo toolchain resolves there. See
[COMPATIBILITY.md](https://github.com/mikn/rules_typescript/blob/main/COMPATIBILITY.md#windows).

## compilerOptions.X is set by the rule and cannot be overridden

```
ts_compile: compilerOptions.paths is set by the rule and cannot be overridden --
use path_aliases for source aliases, or module_name on the target that produces
the declarations.
Remove "paths" from compiler_options on //src/app:app.
```

Sixteen `compilerOptions` keys encode the sandbox layout or the action's declared
outputs, and `compiler_options` rejects all sixteen. The message names the
attribute to use; the full list is in
[ts_compile](../rules/ts-compile.md#the-two-hard-errors).

## path_aliases points into the output tree

```
ts_compile: path_aliases["@acme/ui"] on //src/app:app points into the output
tree (bazel-out/k8-fastbuild/bin/packages/ui).
```

A path under `bazel-out/` embeds the build configuration, so it stops resolving
under `-c opt` or a different exec platform. `path_aliases` is for **source**
directories. To import another target by bare specifier, set `module_name` on
the target that produces its declarations and depend on it.

## path_aliases points at a directory where none of this target's inputs live

```
ts_compile: path_aliases["@/"] on @@//src/app:app points at "./src/", where none
of this target's inputs live.
```

Gazelle writes a `path_aliases` entry for every alias an import resolves through,
and `ts_compile` accepts an alias only when it resolves to files this target
stages. A cross-package alias never does, so the near-universal
`"@/*": ["src/*"]` fails on the first build after Gazelle.

Cross-package imports are `module_name`'s job. Set it on the target that produces
the files, and drop the alias from the consumer with a `# keep` above the rule,
because Gazelle re-derives the attr from `tsconfig.json` on every run:

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
    srcs = ["main.ts"],          # import { add } from "@/lib/math";
    deps = ["//src/lib"],
)
```

The sources and the `tsconfig.json` entry keep the `@/lib/math` specifier.
`path_alias_srcs` is the other way out: list the files the alias resolves to.
Practical only in the same Bazel package, since a file elsewhere has to be
exported before any label can reference it.

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

## imports a module no direct dep provides

```
ERROR: .../src/app/BUILD.bazel:3:11: TsStrictDeps //src/app:app failed: (Exit 1)
//src/app:app imports a module no direct dep provides:

  src/app/main.ts:1  imports "zod"
                     add "@npm//:zod" to deps
```

The import resolves today only because it reaches this target through another
dep's own deps, and stops resolving the moment that dep drops it. Add the label
the message names, or run Gazelle:

```bash
bazel run //:gazelle
```

The check and Gazelle share one specifier scanner, so a Gazelle run that leaves
a reported failure unfixed is a bug. Two things are outside the check:
`/// <reference types="x" />`, for which Gazelle generates no dep either, and an
import nothing in the closure provides at all, which TypeScript reports as
`TS2307`.

## Option 'baseUrl' has been removed

```
pkg/tsconfig.json(3,5): error TS5102: Option 'baseUrl' has been removed.
Please remove it from your configuration.
```

`baseUrl` is gone in tsgo, and the generated config cannot take it back out of
your file: the diagnostic fires on the key being *present* anywhere in the
`extends` chain, so re-stating it above yours only moves the error. Delete it
from the `tsconfig.json` the target names — `paths` here is Bazel's, rewritten
per configuration, so nothing in this ruleset resolves against a `baseUrl`
anyway. `compiler_options` rejects the key at analysis for the same reason; use
`path_aliases` for a source alias.

## Option 'moduleResolution' must be set to 'NodeNext'

```
pkg/tsconfig.json(2,3): error TS5109: Option 'moduleResolution' must be set to
'NodeNext' (or left unspecified) when option 'module' is set to 'NodeNext'.
```

Two layers each supplying one half of a coupled pair. The ruleset states
`moduleResolution` only where it also owns the `module` it belongs to, so a
`module` of yours never has a stray baseline resolver under it — but two layers
*of yours* still can: a `module` in the `tsconfig.json` the target names and a
`moduleResolution` in `compiler_options` above it, or the same split across your
own `extends` chain. Put both halves in one file, or drop the `moduleResolution`
and let tsgo derive it.

The mirror image, `TS5110`, is a `moduleResolution` of `Node16`/`NodeNext` with
no `module` beside it. Without a `tsconfig` that fails at analysis instead, with
the label and the value to set.

## Import Not Resolving in tsgo

tsgo resolves with `moduleResolution: "Bundler"` — the ruleset's baseline, and
what tsgo derives from every `module` but `Node16`/`NodeNext` — plus `paths`
entries for direct npm deps.
A bare import that resolves nowhere — no `TsStrictDeps` failure, just `TS2307` —
means no dep in the closure provides it at all. Add the package:

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
TypeScript's type-reference resolver, which walks `node_modules/@types` and
`typeRoots`, not the `paths` map that carries npm deps here. There is no
`node_modules` to walk, so no `deps` entry makes it resolve.

One caveat: while `bazel run //:dev` is running there *is* a `node_modules` at
the workspace root — the dev server links the npm tree in so bare specifiers
resolve (see [Dev Server](dev-server.md#how-a-bare-npm-specifier-resolves)). Your
editor may then resolve this directive, and bare imports with no `deps` entry,
for as long as the server runs. `bazel build` never does; a green editor and a
red build means the link is what your editor found.

Delete the directive and ask for the same globals through the rule:

```python
ts_compile(
    name = "app",
    srcs = ["src/main.ts"],
    vite_types = True,   # import.meta.env, import.meta.hot, asset imports
)
```

`vite_types` prepends a standalone ambient shim: it declares the Vite client
globals without referencing `vite/client`, so `vite` does not become a
compile-time dependency. It is a src like any other, so it types the target
that sets the attribute and no other -- a consumer using `import.meta.env` sets
`vite_types = True` itself. Anything else the directive was reaching for is an
ordinary `@types/*` package. Name it in `deps`, or in
[`# gazelle:ts_ambient_types`](../gazelle/directives.md#declare-ambient-types-once-for-the-whole-repo)
if the whole tree needs it.

Gazelle does not rewrite the directive, and neither the import scanner nor the
strict-deps checker reads it. That is the directive in a file of your own; one
in an npm package's declaration entry -- `@types/bun/index.d.ts` is exactly
`/// <reference types="bun-types" />` -- is followed by the rule, see
[`@types/*` packages](../rules/ts-compile.md#types-packages).

## compilerOptions.types entry "vite/client" resolves to nothing

```
ts_compile: compilerOptions.types entry "vite/client" on @@//app:app resolves to
nothing.
No dep of this target publishes "vite", and a `types` entry is resolved from
this target's own deps -- there is no node_modules for TypeScript to walk.
```

A `types` entry names a package, and the rule resolves it against this target's
`deps`, putting the declaration the package's manifest designates into the
generated config's `files`. Add the dep that publishes it:

```python
ts_compile(
    name = "app",
    srcs = ["app.ts"],
    compiler_options = {"types": ["vite/client"]},
    deps = ["@npm//:vite"],
)
```

The message names the subpaths a package that is already a dep does designate,
and any dep whose name is near the entry's. Two other ways out: name a
declaration file instead (`types = ["./worker-configuration.d.ts"]`, with the
file in `types_srcs` -- see below), or state a `typeRoots` in
`compiler_options`, which exempts the target from this check -- what sits under
a `typeRoots` is the compiler's to find at action time.

It fails at analysis because nothing downstream would say it. `tsc` reports
`TS2688` for such an entry; tsgo, the compiler this ruleset runs, reports
nothing at all and exits 0, so the target compiles without the declarations and
the error lands on whatever needed them -- `TS2339` on `import.meta.env` without
`vite/client`, `TS2591` on `process` without `node`.

## compilerOptions.types entry names a path no file of mine sits at

```
ts_compile: compilerOptions.types entry "../../worker-configuration.d.ts" on
@@//workers/proxy/src/lib:lib names "workers/proxy/worker-configuration.d.ts",
  which no file this target stages sits at.
```

A relative `types` entry is a path, and a path resolves against the sandbox:
only what this target's action stages is in it -- its `srcs`, its `types_srcs`,
its `path_alias_srcs`, its deps' declarations, checked in or generated. Name
the file with a label:

```python
ts_compile(
    name = "lib",
    srcs = glob(["*.ts"]),
    types = ["../../worker-configuration.d.ts"],
    types_srcs = ["//workers/proxy:worker-configuration.d.ts"],
)
```

`types_srcs` stages the file for the entry to resolve and does not publish it
as this target's own declaration, which listing it in `srcs` would. tsgo parses
it as part of this program either way, so a syntax error in the file fails this
target; what it declares goes unchecked under the baseline's `skipLibCheck`.

It fails at analysis for the same reason the package shape does: tsgo reports
nothing for a `types` entry that resolves to nothing, so the target used to
compile against a smaller type environment than it asked for and the error
landed on whatever needed the globals -- `TS2304: Cannot find name` on a
Worker's `Env`. A `typeRoots` does not exempt this shape: `./x.d.ts` and
`../x.d.ts` are resolved against the config's own directory, never through
`typeRoots`.

## types_srcs names a file no compilerOptions.types entry names

The mirror of it. `types_srcs` stages a declaration for an entry to resolve and
is not `include`, so a file no entry names reaches the program by no route at
all. Name it in `types`, or drop the label.

## The `types` in my tsconfig file does nothing

Only the `types` in `compiler_options` is resolved and guarded. In the
`tsconfig` file a target names it is a layer the rule does not read, so those
entries reach tsgo unresolved: a target whose `tsconfig` holds
`"types": ["vite/client"]` and whose `deps` hold `@npm//:vite` analyses with no
complaint, generates a config whose `files` is empty, and fails in tsgo with
`TS2339` on the `import.meta.env` the declarations that never arrived would have
typed. Move the entries to `compiler_options`.

## ts_test: vitest not found

```
ts_test: vitest not found. Set the vitest attr or add @npm//:vitest to the deps
of the node_modules() target this test uses.
```

`vitest` has to be reachable from the tree the test runs against. With the
default (auto) `node_modules`, list it in `deps`:

```python
ts_test(
    name = "my_test",
    srcs = ["my.test.ts"],
    deps = [":my_lib", "@npm//:vitest"],
)
```

With an explicit `node_modules` target, put it there; the auto generation is
skipped entirely when `node_modules` is set:

```python
node_modules(
    name = "node_modules",
    deps = ["@npm//:vitest"],
)

ts_test(
    name = "my_test",
    srcs = ["my.test.ts"],
    deps = [":my_lib"],
    node_modules = ":node_modules",
)
```

The same applies to every package the run needs at runtime, including ones only
the production code imports: the auto tree is built from `ts_test`'s own npm deps
and their transitive npm deps, and a `ts_compile` dep does not contribute its
own.

## Isolated declarations error: missing return type

Reachable only under `declarations = "oxc"`, where Oxc derives `.d.ts` from
syntax and so needs an explicit type on every export:

```
× Isolated declarations error(s): TS9013: Expression type can't be inferred
│ with --isolatedDeclarations.
```

Add the annotation Oxc names, or drop that target back to the default
`declarations = "tsgo"`, where the compiler infers it. See
[Isolated Declarations](../getting-started/isolated-declarations.md).

## Type errors are not failing the build

Under the default `declarations = "tsgo"` they always do: the `.d.ts` are real
outputs of the type-checking action, so a target with a type error produces
nothing.

Under `declarations = "oxc"`, type-checking moves into the `_validation` output
group, off the critical path. Bazel runs those actions during `bazel build` on
its own, unless `--norun_validations` turns them off; the `.bazelrc` line the
quickstart writes requests the group explicitly:

```
build --output_groups=+_validation
```

With `enable_check = False` nothing type-checks the target. Under `"oxc"` the
declarations are still complete, because Oxc enforces isolated declarations
itself; under `"tsgo"` the target emits no `.d.ts`, intended for terminal targets
whose declarations nothing consumes.

## Gazelle Generating Wrong Deps

If Gazelle generates incorrect `deps` for an import:

1. Check that the import specifier matches an npm package name in the lockfile.
2. For path aliases, check `compilerOptions.paths` in the nearest
   `tsconfig.json` — Gazelle reads it directly, as JSONC — or set
   `# gazelle:ts_path_alias @/ src/` explicitly.
3. Use `# gazelle:ts_ignore` to suppress generation for a directory and write
   its BUILD file manually.

## typescript: &lt;framework&gt; detected: bundling it is unsupported

Not an error. Gazelle recognised a framework it generates no bundle target for —
currently SolidStart, and nothing else — and the message carries the reason. The
rest of the workspace still compiles and tests. For a client-only build, declare
a `ts_bundle` by hand with no `vite_config`. See
[Framework detection](../gazelle/overview.md#framework-detection).

## rule '//app:entry_client' does not exist, after Gazelle on a framework workspace

The generated framework `ts_bundle` names a single-file entry target in the
package holding the framework's client entry: for Remix, `app/entry.client.tsx`
becomes `//app:entry_client`. Nothing declares that name when no source file maps
to it, or when a `# gazelle:ts_exclude` drops the one that would. Gazelle then
generates no `ts_bundle` and withdraws one it wrote earlier, since an unresolvable
label fails analysis for every target that reaches it:

```
typescript: Remix detected: ts_bundle(app_remix) is being withdrawn -- its
entry_point "//app:entry_client" names a target nothing declares any more, and
an unresolvable label fails analysis for every target that reaches it. A bundle
you maintain yourself needs a "# keep" comment above the rule to survive this.
typescript: Remix detected: nothing in app/ declares the client entry target
"entry_client" -- no source file there maps to that name, or a ts_exclude
directive drops it -- so no app_remix bundle target was generated: entry_point
//app:entry_client would name nothing, and an unresolvable label fails analysis
for every target that reaches it. Add the framework's client entry there, drop
the exclusion, or declare the bundle by hand with a "# keep" comment above the
rule -- without one the next run that does find an entry rewrites it.
```

The missing rule therefore belongs to a bundle Gazelle is not maintaining: one
carrying a `# keep`, or one whose `entry_point` you pointed at a target of your
own. Setting `entry_point` by hand does not help; the next run that finds an
entry rewrites it.

Put the framework's client entry where the framework expects it, drop any
`# gazelle:ts_exclude` covering it, and re-run Gazelle: it writes the single-file
`ts_compile`, the `sources` filegroup and the bundle.

Declaring the pair by hand also works. Gazelle owns both attributes, so each rule
needs a `# keep` to survive later runs, and `deps` stop tracking the entry's
imports:

```python
# app/BUILD.bazel
# gazelle:ts_exclude entry.client.tsx

load("@rules_typescript//ts:defs.bzl", "ts_compile")

# keep
ts_compile(
    name = "entry_client",
    srcs = ["entry.client.tsx"],
    deps = ["@npm//:remix-run_react"],  # yours to keep current
    visibility = ["//visibility:public"],
)

filegroup(
    name = "sources",
    srcs = ["entry.client.tsx"],  # plus the package's other sources
    visibility = ["//visibility:public"],
)
```

`ts_exclude` takes the file out of every generated target, so the `sources`
filegroup puts it back for the framework, which reads its client entry from the
staging root by name. See
[The entry point is generated](../gazelle/overview.md#the-entry-point-is-generated).

## ts_dev_server: has no node_modules attr

```
ts_dev_server: @@//src/app:dev has no node_modules attr, so the app's own
dependencies are not in runfiles.
Add node_modules = ":node_modules" pointing at a node_modules() target; the
generated config resolves every bare specifier through that tree.
```

Gazelle generates the `ts_dev_server` target with `plugin` set and
`node_modules` empty, because nothing in the source tree says which tree the app
resolves against. Declare it in the dev server's own Bazel package, listing
every npm package the app imports, plus `@npm//:vite` under the default server;
an oj target needs the app's own packages and no vite. Gazelle leaves the attr
alone from then on:

```python
node_modules(
    name = "node_modules",
    deps = ["@npm//:vite", "@npm//:zod"],
)
```

Name it `node_modules` — see the next entry.

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
shows the package sitting right there in the tree.

Rename the target to `node_modules` (one per Bazel package) and update the attr
pointing at it. `@vitejs/plugin-react` fails the same walk-up for its
`react-refresh` runtime, so `react_refresh = True` needs the same name.

## [vite]: failed to resolve import "…" from …/bazel-out/…/bin/…

```
Error: [vite]: Rolldown failed to resolve import "zod" from
"…/bazel-out/k8-fastbuild/bin/src/lib/math.js".
```

("Rollup" in place of "Rolldown" before Vite 8; the cause is the same.)

A `ts_bundle` whose bundler's `node_modules` tree cannot answer a bare specifier
in the bundled graph. Two causes, the first the common one:

- **the package is not in the tree.** The tree supplies Vite and every npm
  package anything in the graph imports; a `ts_compile` dep does not contribute
  its own. Add it to the `node_modules` target's `deps`;
- **the tree is in the wrong place.** It is materialised under the bundler's
  package in `bazel-bin`, and rolldown resolves from the importer by Node's
  walk-up, so the tree has to be an ancestor of every compiled `.js` doing the
  importing. A bundle in `//bundle` cannot serve `bin/src/lib/math.js`. Declare
  `node_modules`, `vite_bundler` and `ts_bundle` at the workspace root.

`external` is a third option, leaving the specifier as an import for whoever
consumes the bundle. See
[where the bundler's node_modules has to sit](bundling.md#where-the-bundlers-node_modules-has-to-sit).

## ts_dev_server: sets react_refresh = True, but @vitejs/plugin-react did not load

The dev server does not start: it could not load the Fast Refresh plugin out of
the Bazel `node_modules` tree, and the message ends with the underlying cause.
Almost always the package is not in the tree; add it to the `node_modules`
target the dev server uses:

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

Under oj this error does not arise. `react_refresh = True` is rejected at
analysis time instead, because oj applies Fast Refresh itself and stacking
`@vitejs/plugin-react` on top would instrument every component twice.

## [rules_typescript] Failed to load vite_config

`ts_dev_server` and `ts_bundle` both load a **copy** of your `vite_config` from
`bazel-bin`, so the file's own imports resolve beside the Bazel npm tree, not in
your source tree. Staged there are the config and the modules `vite_config_srcs`
declares, nothing else:

- a **relative** import of a module not in `vite_config_srcs` fails, and the
  message names the file. Declare it:

  ```python
  vite_config = "vite.plugins.ts",
  vite_config_srcs = glob(["plugins/**/*.ts"]),
  ```

  A module outside the config's own Bazel package cannot be declared, since it
  would have to stage above the staging root; that is a separate analysis-time
  error naming the file and the package;
- a **bare npm** import works, as long as the `node_modules` target is in the same
  Bazel package as the dev server. Move it back beside the server, or add a
  `node_modules` target there.

See [`vite_config`: what it may import](dev-server.md#vite_config-what-it-may-import).

## [rules_typescript] ts_bundle: the vite_config sets …, which the generated config does not read

The generated config reads a fixed set of keys out of your `vite_config`, and the
set differs between the two rules because the dev server takes its serve root
from elsewhere:

| Rule | Keys it reads |
|---|---|
| `ts_bundle` | `plugins`, `root` |
| `ts_dev_server` | `plugins` |

Every other key would be silently discarded, so the load throws, naming the keys
it found and the keys it honours. A framework config that sets `define`,
`resolve.alias`, `build.target` or `optimizeDeps` hits this, as does one carrying
`root` that builds under `ts_bundle` and fails under `ts_dev_server`. Move what
you need into a plugin, or use the `ts_bundle` attribute that owns it (`define`,
`env_vars`, `external`, `minify`, `split_chunks`). The check runs at config-load
time, not analysis time, because only the loaded object says what keys it has.

## Dev server: Failed to resolve import "some-package"

The dev server resolves bare specifiers through the `node_modules` tree only, and
the package is not in that tree. Add it to the target's `deps` and restart:

```python
node_modules(
    name = "node_modules",
    deps = ["@npm//:vite", "@npm//:some-package"],
)
```

A first-party package name needs `module_name` on the `ts_compile` target that
produces it; the dev server turns each one into a `resolve.alias` pointing at
source.

## gazelle: typescript: paths entry "…" resolves on disk to N directories

Not an error, but something is being dropped. `compilerOptions.paths` values are
arrays and a `path_aliases` entry holds one directory, so Gazelle picks one:
`bazel-*` and tool-managed dot-directory entries are skipped, then the first
entry that exists on disk wins. The line fires only when two or more entries in
one chain are real directories.

A specifier reaching only through an ignored directory gets no dep edge, and the
`tsconfig.json` `ts_compile` generates carries no such directory either, so the
type-check fails on it too. Split the alias so each key names one directory, list
the extra files in `path_alias_srcs`, or set `module_name` on the target that
produces them and depend on it.

Two cases are skipped without a log line: the `./bazel-bin/…` mirror
`ts_refresh_tsconfig` writes beside each source entry, and a chain whose entries
are all absent from the working tree, where the first entry is used as before.

## invalid repository name '{$username}.tsx'

A source file whose name starts with `@` — a TanStack Start route on a dynamic
segment is written `@{$username}.tsx` — reached a `srcs` list bare. A `srcs`
entry is a label, and Bazel reads the head of the string rather than the file
system: `@` opens a repository name, `//` an absolute package, and a `:`
anywhere splits package from target. So the bare name names a repository that
does not exist, and one such entry fails `bazel query //...` for every package
in the workspace rather than for the one target.

Gazelle writes `":@{$username}.tsx"` for these, which pins the name to the
package the way a bare name does for every other file. Hand-written BUILD files
need the same leading colon.

A name holding a `:` has no label in any spelling — a target name may not
contain one — and Gazelle leaves such a file out of every target it generates,
saying so on one line per file. Rename the file.

**`exports_files` wants the opposite spelling.** Its argument is a list of
target names, not of labels, so the bare `exports_files(["@{$username}.tsx"])`
is the correct form there and `exports_files([":@{$username}.tsx"])` fails with
`target names may not contain ':'` — misleading, since the colon is the fix one
attribute over. Applying the `srcs` rule here is what produces that error. A
`filegroup` whose `srcs` is a `glob()` avoids the question entirely: a glob
pattern is matched against the file system rather than parsed as a label.

## Snapshot 'x 1' mismatched, or a snapshot vitest says is new

`ts_test` runs vitest in read-only snapshot mode, so a mismatch is a failure and
a snapshot the sandbox cannot read counts as absent. Two causes:

- The `.snap` is not in `snapshots`, so it never reached the runfiles tree. Add
  `snapshots = glob(["__snapshots__/*.snap"])`.
- The snapshot is genuinely stale. Regenerate it:
  `bazel run //path/to:my_test.update_snapshots`, then commit.

Full workflow: [Snapshots](testing.md#snapshots).

## Slow First Build

The first build downloads a Rust toolchain, a tsgo npm tarball, a Node.js
tarball, and the npm packages your targets reach — not the whole lockfile — then
compiles `oxc-bazel` and its crate graph from Rust source. That compile is the
long pole: budget minutes, and do not `bazel clean` afterwards. Everything after
it is cached.

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
builds `oxc-bazel` natively and `@typescript/native-preview-linux-arm64` provides
tsgo.
