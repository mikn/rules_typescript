# Troubleshooting

## No repository visible as '@rules_rust'

```
ERROR: @rules_rust//rust/toolchain/channel :: Error loading option
@rules_rust//rust/toolchain/channel: No repository visible as '@rules_rust'
from main repository
```

A `.bazelrc` in your workspace sets a `--@rules_rust//...` flag. Delete the
line. `rules_rust` is a transitive dependency of `rules_typescript`, so
`@rules_rust` is not in your module's repo mapping and Bazel cannot resolve
the flag's label. Nothing in a consumer's `.bazelrc` needs it: the Rust
toolchain channel already defaults to `stable`, which is the only channel
`rules_typescript` registers a toolchain for.

If you really do need `@rules_rust` flags for Rust code of your own, add
`bazel_dep(name = "rules_rust", version = "0.69.0")` to your `MODULE.bazel` to
bring the repo into your mapping — but note that the flag then applies to the
`oxc-bazel` build too, and a non-`stable` channel will fail toolchain
resolution.

## No repository visible as '@pnpm'

```
ERROR: no such package '@@[unknown repo 'pnpm' requested from @@ (did you mean
'npm'?)]//': No repository visible as '@pnpm' from main repository and
referenced by '//:pnpm'
```

Gazelle writes `ts_pnpm(name = "pnpm")` and `ts_add_package(name =
"add_package")` into your root `BUILD.bazel` as soon as a root `pnpm-lock.yaml`
exists — unconditionally, whether or not you want the hermetic-pnpm workflow —
and both name `@pnpm`. Take the repo:

```python
npm = use_extension("@rules_typescript//npm:extensions.bzl", "npm")
npm.translate_lock(pnpm_lock = "//:pnpm-lock.yaml")
use_repo(npm, "npm", "pnpm")
```

`npm.pnpm(version = …)` is not required with it — a default version is used.
Nothing runs those two targets on your behalf, and they cost nothing until you
do. See [npm Dependencies § Setup](npm.md#setup).

## No repository visible as '@npm'

```
ERROR: no such package '@@[unknown repo 'npm' requested from @@]//': No
repository visible as '@npm' from main repository and referenced by
'//src/lib:_lib_test_node_modules'
```

Gazelle resolved a bare import to an `@npm//:…` label and there is no hub yet.
Any project with a single npm dependency needs the extension before its first
build — the three lines above. If the label names a *second* hub
(`@npm_tools//:…`), that hub is missing from `use_repo`; see
[more than one hub](npm.md#more-than-one-hub).

## No test targets were found, yet testing was requested

```
INFO: Found 2 targets and 0 test targets...
ERROR: No test targets were found, yet testing was requested
```

Not a broken setup. `bazel test //...` is an error (exit code 4) when the pattern
matches no test target, and a project with no `*.test.ts` / `*.spec.ts` yet has
none. Gazelle generates a `ts_test` from the first such file it sees; vitest
comes from your lockfile, so
[the quickstart's step 9](../getting-started/quickstart.md#path-a-new-project)
walks the whole first-test path.

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

Toolchain registration is the **consumer's** job. `rules_typescript` deliberately
registers nothing on your behalf, the same way `rules_go` does not. Your
`MODULE.bazel` needs:

```python
register_toolchains("@rules_typescript//ts/toolchain:all")
```

Without it, nothing resolves the tsgo toolchain, and under the default
`declarations = "tsgo"` that means no `.d.ts` and no type-checking.

To pin a tsgo version:

```python
ts = use_extension("@rules_typescript//ts:extensions.bzl", "ts")
ts.tsgo(version = "7.0.0-dev.20260311.1")
```

Windows is not supported, so no tsgo toolchain resolves there — see
[COMPATIBILITY.md](https://github.com/mikn/rules_typescript/blob/main/COMPATIBILITY.md#windows).

## compilerOptions.X is set by the rule and cannot be overridden

```
ts_compile: compilerOptions.paths is set by the rule and cannot be overridden --
use path_aliases for source aliases, or module_name on the target that produces
the declarations.
Remove "paths" from compiler_options on //src/app:app.
```

Sixteen `compilerOptions` keys encode the sandbox layout or the action's
declared outputs, so `compiler_options` refuses them rather than applying a
value that would break the build. The message names the attribute to use
instead; the full list is in
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

The near-universal `"@/*": ["src/*"]` in an existing project's `tsconfig.json`,
met on the first build after Gazelle. Gazelle writes a `path_aliases` entry for
every alias an import resolves through, and `ts_compile` accepts an alias only
when it resolves to files *this* target stages — which a cross-package alias
never does.

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

Your sources and your editor keep the `@/lib/math` specifier and the
`tsconfig.json` entry behind it. The other way out is `path_alias_srcs`, listing
the files the alias resolves to — practical only when they are in the same Bazel
package, since a source file in another package has to be exported to be
referenced at all.

## npm: pnpm-lock.yaml declares patchedDependencies with no patch file

Your lockfile patches a package and the extension was not given the patch. Pass
it as a label:

```python
npm.translate_lock(
    pnpm_lock = "//:pnpm-lock.yaml",
    patches = ["//patches:@acme__diffs@1.3.1.patch"],
)
```

The reverse — a patch file no `patchedDependencies` entry claims — fails too,
and means the lockfile is stale or the file is misnamed. Names follow
`pnpm patch-commit`: `<name with / replaced by __>@<version>.patch`. See
[npm Dependencies](npm.md#patched-dependencies).

## npm: patch labels that resolve to no readable file

```
npm: patch labels passed to npm.translate_lock(patches = [...]) that resolve to
no readable file:
  @@//patches:@acme__diffs@1.3.1.patch
```

The label is wrong, the file is missing, or the Bazel package holding it failed
to load. The last one has a specific cause worth knowing: a patch filename
starting with `@` cannot be exported by `exports_files(glob(["*.patch"]))` —
`glob()` prefixes `:` onto such a result and `exports_files` rejects it as a
target name, which fails the whole package and every patch in it. List those
files literally:

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
file. Bazel refuses to apply a patch pnpm never saw.

## node_modules: depends on two versions of one name at once

```
node_modules: @@//src/app:node_modules depends on two versions of 'minimatch' at once:
  minimatch@10.2.4
  minimatch@9.0.9
```

`node_modules/<name>` is one directory, so no arrangement of the tree answers
`import "<name>"` with both. Depend on one of them here and let the other arrive
through the package that needs it — a version reached transitively keeps its own
version, in its own store directory — or split the two into separate
`node_modules` targets. See
[node_modules](../rules/node-modules.md#two-resolutions-of-one-name-in-deps).

The same error has a narrower form, for two resolutions of *one* version that
differ in the peers pnpm resolved them against:

```
node_modules: @@//src/app:node_modules depends on two resolutions of
'ansi-styles@6.2.3' at once, one per peer set:
  peers ansi-regex_5_0_1_5e7443ea
  peers ansi-regex_6_2_2_602a0566
```

There is no arrangement for that one either. The tarball is the same both ways;
what differs is what the package's own dependencies resolve to, and that lives in
the one `node_modules/<name>/node_modules`.

## imports a module no direct dep provides

```
ERROR: .../src/app/BUILD.bazel:3:11: TsStrictDeps //src/app:app failed: (Exit 1)
//src/app:app imports a module no direct dep provides:

  src/app/main.ts:1  imports "zod"
                     add "@npm//:zod" to deps
```

The import resolves today only because it reaches this target through another
dep's own deps, and stops resolving the moment that dep drops it. Add the label
the message names, or let Gazelle write it:

```bash
bazel run //:gazelle
```

If Gazelle does *not* write it, that is a bug worth reporting: the check and
Gazelle share one specifier scanner precisely so that every failure it reports
is one Gazelle can fix. Two things are deliberately outside the check —
`/// <reference types="x" />` (Gazelle generates no dep for it either) and an
import nothing in the closure provides at all, which TypeScript reports as
`TS2307` because there is no label to suggest.

## Import Not Resolving in tsgo

tsgo uses `moduleResolution: "Bundler"` with `paths` entries for direct npm
deps. A bare import that resolves nowhere — no `TsStrictDeps` failure, just
`TS2307` — means no dep in the closure provides it at all. Add the package:

```python
ts_compile(
    name = "app",
    srcs = ["app.ts"],
    deps = ["@npm//:zod"],
)
```

## ts_test: vitest not found

```
ts_test: vitest not found. Set vitest attr or include it in node_modules.
```

`vitest` has to be reachable from the tree the test runs against. With the
default (auto) `node_modules`, that means listing it in `deps`:

```python
ts_test(
    name = "my_test",
    srcs = ["my.test.ts"],
    deps = [":my_lib", "@npm//:vitest"],
)
```

With an explicit `node_modules` target, put it there instead — the auto
generation is skipped entirely when `node_modules` is set:

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
the production code imports: the auto tree is built from `ts_test`'s direct npm
deps and their transitive npm deps, and a `ts_compile` dep does not contribute
its own.

## Isolated declarations error: missing return type

Only reachable under `declarations = "oxc"`, where Oxc derives `.d.ts` from
syntax and so needs an explicit type on every export:

```
× Isolated declarations error(s): TS9013: Expression type can't be inferred
│ with --isolatedDeclarations.
```

Two ways out. Add the annotation Oxc names, or drop that target back to the
default `declarations = "tsgo"`, where the compiler infers it. See
[Isolated Declarations](../getting-started/isolated-declarations.md).

## Type errors are not failing the build

Under the default `declarations = "tsgo"` they always do — the `.d.ts` are real
outputs of the type-checking action, so a target with a type error produces
nothing.

If a target sets `declarations = "oxc"`, type-checking is a validation action
instead, and Bazel only runs those when asked:

```
build --output_groups=+_validation
```

If a target sets `enable_check = False`, nothing type-checks it at all. Under
`"oxc"` that still gives you complete declarations (Oxc enforces isolated
declarations itself); under `"tsgo"` it means the target emits no `.d.ts`,
which is intended for terminal targets whose declarations nothing consumes.

## Gazelle Generating Wrong Deps

If Gazelle generates incorrect `deps` for an import:

1. Check that the import specifier matches an npm package name in the lockfile.
2. For path aliases, check `compilerOptions.paths` in the nearest
   `tsconfig.json` — Gazelle reads it directly, as JSONC — or set
   `# gazelle:ts_path_alias @/ src/` explicitly.
3. Use `# gazelle:ts_ignore` to suppress generation for a directory and write
   its BUILD file manually.

## typescript: &lt;framework&gt; detected: bundling it is unsupported

Not an error. Gazelle recognised a framework it deliberately generates no bundle
target for — currently Solid Start, and nothing else — and named the reason
rather than writing a `ts_bundle` that cannot build. The rest of the workspace still
compiles and tests. For a client-only build, declare a `ts_bundle` by hand with
no `vite_config`. Details:
[Framework detection](../gazelle/overview.md#framework-detection).

## rule '//app:entry_client' does not exist, after Gazelle on a framework workspace

The generated framework `ts_bundle` names a single-file entry target, which
Gazelle writes in the package that holds the framework's client entry — for
Remix, `app/entry.client.tsx` becomes `//app:entry_client`. Nothing in that
package declares that name when no source file maps to it, or when a
`# gazelle:ts_exclude` drops the one that would.

Gazelle does not leave a bundle naming a label like that behind: it generates no
`ts_bundle` at all, and withdraws one it wrote earlier, because an unresolvable
label takes down `bazel build //...` for the whole workspace rather than that one
target. The run says both halves:

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

So the missing rule reaches you from a bundle Gazelle is not maintaining: one
carrying a `# keep`, or one whose `entry_point` you pointed at a target of your
own. Setting `entry_point` by hand on the generated bundle is not the escape —
there is no generated bundle to set it on, and the next run that does find an
entry rewrites the attribute.

Put the framework's client entry where the framework expects it, drop any
`# gazelle:ts_exclude` covering it, and re-run Gazelle: it writes the single-file
`ts_compile`, the `sources` filegroup and the bundle itself.

Declaring the target by hand instead still works, but Gazelle then maintains
neither it nor the bundle's `entry_point`, and both are attributes it owns — so
the hand-written pair needs a `# keep` above each to survive later runs, and its
`deps` stop tracking the entry's imports:

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

The `sources` filegroup is there because `ts_exclude` takes the file out of every
generated target, that one included, and the framework reads its client entry
out of the staging root by name. Details:
[The entry point is generated](../gazelle/overview.md#the-entry-point-is-generated).

## ts_dev_server: has no node_modules attr

```
ts_dev_server: @@//src/app:dev has no node_modules attr, so the app's own
dependencies are not in runfiles.
Add node_modules = ":node_modules" pointing at a node_modules() target; the
generated config resolves every bare specifier through that tree.
```

Gazelle generates the `ts_dev_server` target with `plugin` set and
`node_modules` empty — nothing in the source tree says which tree the app should
resolve against — so the first `bazel run` on a freshly generated target stops
here. Declare the tree in the dev server's own Bazel package, listing Vite plus
every npm package the app imports, and Gazelle leaves the attr alone from then
on:

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
by walking up for a directory called `node_modules` and nothing else — so Vite 8,
whose entry imports `rolldown` that way, does not start. The package is right
there in the tree, which is what the "Did you mean" line is saying.

Rename the target to `node_modules` (one per Bazel package) and update the
`node_modules` attr that points at it. `@vitejs/plugin-react` fails the same
walk-up for its `react-refresh` runtime, so `react_refresh = True` needs the same
name.

## [vite]: failed to resolve import "…" from …/bazel-out/…/bin/…

```
Error: [vite]: Rolldown failed to resolve import "zod" from
"…/bazel-out/k8-fastbuild/bin/src/lib/math.js".
```

("Rollup" rather than "Rolldown" before Vite 8; the cause is the same.)

A `ts_bundle` whose bundler's `node_modules` tree cannot answer a bare specifier
in the bundled graph. Two separate causes, and the first is the common one:

- **the package is not in the tree.** It supplies Vite *and* every npm package
  anything in the graph imports; a `ts_compile` dep does not contribute its own.
  Add it to the `node_modules` target's `deps`;
- **the tree is in the wrong place.** It is materialised under the *bundler's*
  package in `bazel-bin`, and rolldown resolves from the importer by Node's
  walk-up — so the tree has to sit in a directory that is an ancestor of every
  compiled `.js` doing the importing. A bundle in `//bundle` cannot serve
  `bin/src/lib/math.js`, and neither can one in `//src/app` when the import is in
  `//src/lib`. Declare `node_modules`, `vite_bundler` and `ts_bundle` at the
  workspace root.

Full explanation:
[where the bundler's node_modules has to sit](bundling.md#where-the-bundlers-node_modules-has-to-sit).
The third option is `external`, which leaves the specifier as an import for
whoever consumes the bundle.

## ts_dev_server: sets react_refresh = True, but @vitejs/plugin-react did not load

The dev server refuses to start because it could not load the Fast Refresh plugin
out of the Bazel `node_modules` tree. Almost always the package is not in the
tree — add it to the `node_modules` target the dev server uses:

```python
node_modules(
    name = "node_modules",
    deps = [
        "@npm//:vite",
        "@npm//:vitejs_plugin-react",
    ],
)
```

The message ends with the underlying cause, and the target name matters too: the
plugin resolves the `react-refresh` runtime by Node's own walk-up, which only
looks in directories called `node_modules`. This used to be a `console.warn` and a
server that came up without Fast Refresh; it is a hard failure now, on purpose.

## [rules_typescript] Failed to load vite_config

`ts_dev_server` and `ts_bundle` both load a **copy** of your `vite_config` from
`bazel-bin`, so the file's own imports resolve beside the Bazel npm tree rather
than in your source tree. What is staged there is the config plus the modules
`vite_config_srcs` declares, and nothing else:

- a **relative** import of a module that is *not* in `vite_config_srcs` fails —
  the message names the file it could not find. The fix is to declare it:

  ```python
  vite_config = "vite.plugins.ts",
  vite_config_srcs = glob(["plugins/**/*.ts"]),
  ```

  A module outside the config's own Bazel package cannot be declared, because it
  would have to stage above the staging root; that is a separate analysis-time
  error naming the file and the package;
- a **bare npm** import works, as long as the `node_modules` target is in the same
  Bazel package as the dev server. If you moved one of them, move it back or add a
  `node_modules` target beside the server.

Details: [`vite_config`: what it may import](dev-server.md#vite_config-what-it-may-import).

## [rules_typescript] ts_bundle: the vite_config sets …, which the generated config does not read

The generated config reads a fixed set of keys out of your `vite_config` — and
they differ between the two rules, because the dev server takes its serve root
from elsewhere:

| Rule | Keys it reads |
|---|---|
| `ts_bundle` | `plugins`, `root` |
| `ts_dev_server` | `plugins` |

Every other key would be silently discarded, so the load throws instead, naming
the keys it found and the keys it honours. A framework config that sets `define`,
`resolve.alias`, `build.target` or `optimizeDeps` hits this — and so does a
config that carries `root` and builds fine under `ts_bundle` but fails under
`ts_dev_server`. Move what you need into a plugin, or use the `ts_bundle`
attribute that owns it (`define`, `env_vars`, `external`, `minify`,
`split_chunks`). The check runs where the config is loaded rather than at
analysis time, because only the loaded object says what keys it has.

## Dev server: Failed to resolve import "some-package"

The dev server resolves bare specifiers through the `node_modules` tree only.
Nothing is wrong with the config — the package is not in that tree. Add it to the
target's `deps` and restart:

```python
node_modules(
    name = "node_modules",
    deps = ["@npm//:vite", "@npm//:some-package"],
)
```

If the specifier is a first-party package name rather than an npm one, it needs
`module_name` on the `ts_compile` target that produces it; the dev server turns
each one into a `resolve.alias` pointing at source.

## gazelle: typescript: paths entry "…" resolves on disk to N directories

Not an error, but something is being dropped. `compilerOptions.paths` values are
arrays and a `path_aliases` entry holds one directory, so Gazelle picks one:
`bazel-*` and tool-managed dot-directory entries are skipped, then the first
entry that exists on disk wins. This line fires only when two or more entries in
one chain are real directories — the case where the choice loses something.

A specifier that resolves only through an ignored directory gets no dep edge,
and the `tsconfig.json` `ts_compile` generates will not carry that directory
either, so the type-check fails on it too. Split the alias so each key names one
directory, list the extra files in `path_alias_srcs`, or set `module_name` on the
target that produces them and depend on it.

The `./bazel-bin/…` mirror that `ts_refresh_tsconfig` writes beside each source
entry is skipped without a log line; so is a chain whose entries are all absent
from the working tree, where the first entry is used as before.

## Snapshot 'x 1' mismatched, or a snapshot vitest says is new

`ts_test` runs vitest in read-only snapshot mode, so a mismatch is a failure
rather than a rewrite, and a snapshot the sandbox cannot read counts as absent.
Two causes:

- The `.snap` is not in `snapshots`, so it never reached the runfiles tree. Add
  `snapshots = glob(["__snapshots__/*.snap"])`.
- The snapshot is genuinely stale. Regenerate it:
  `bazel run //path/to:my_test.update_snapshots`, then commit.

Full workflow: [Snapshots](testing.md#snapshots).

## Slow First Build

The first build downloads a Rust toolchain, a tsgo npm tarball, a Node.js
tarball, and the npm packages your targets actually reach — not the whole
lockfile — and then compiles `oxc-bazel` and its crate graph from Rust source.
That compile is the long pole: budget minutes, and do not `bazel clean`
afterwards. Everything after it is cached.

To keep paying that once rather than per CI run, mount a persistent cache volume:

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
