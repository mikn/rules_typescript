# Gazelle Overview

Gazelle auto-generates BUILD files from TypeScript source files, inferring `ts_compile` targets and resolving imports to Bazel labels.

## Setup

Add the Gazelle binary to your root `BUILD.bazel`:

```python
load("@gazelle//:def.bzl", "gazelle")

gazelle(
    name = "gazelle",
    gazelle = "@rules_typescript//gazelle:gazelle_typescript",
)
```

`gazelle_typescript` carries the TypeScript language alone. The other binary in
that package, `gazelle_ts`, also carries Go and proto — rules_typescript uses it
to generate BUILD files for its own `.go` sources — so in a polyglot repo it
rewrites Go BUILD files you never asked it about.

Add `gazelle` to `MODULE.bazel`:

```python
bazel_dep(name = "gazelle", version = "0.47.0")
```

`rules_typescript` declares `rules_go`, `go_sdk` and `go_deps` as non-dev
dependencies, so they propagate transitively via bzlmod: consumers need only the
`bazel_dep` above, and no Go toolchain of their own. Building the extension
fetches a Go SDK and its modules, on top of the Rust toolchain `oxc-bazel` needs.

Run Gazelle:

```bash
bazel run //:gazelle
```

### Verifying a Run

Taking Gazelle's output wholesale is the intended workflow.
`//tests/integration:gazelle_roundtrip_test` pins four properties in CI against a
real nested workspace: the output builds, generating it twice from scratch
produces byte-identical BUILD files, `bazel test //...` passes on that output, and
the set of test targets is unchanged across a delete-and-regenerate. It runs on
every pull request and on every push to `main`.

The test-target set is the one worth checking on your own repository too, since a
run that deletes a test still builds and is still idempotent:

```bash
bazel query 'tests(//...)' | sort > before
bazel run //:gazelle
bazel query 'tests(//...)' | sort | diff before -
```

That check exists because seven hand-written `go_test` targets once disappeared,
and the mechanism was Gazelle's Go language rather than the TypeScript extension:
Go turns `# gazelle:exclude *_test.go` into a deletion stub named
`<dirbase>_test`, and a hand-written `go_test` of that name goes with it.

### Getting the Clean-Tree Diff to Empty

Once a repository has settled, a Gazelle run on an unmodified checkout should
change nothing, which is what makes the next non-empty diff mean something.
Check without writing anything:

```bash
bazel run //:gazelle -- -mode=diff
```

Two things commonly keep that diff non-empty on a hand-written BUILD file, and
neither is drift:

- **Gazelle's own rendering.** It writes a one-element list inline
  (`deps = ["//pkg"]`) and names a generated file by its producing label where you
  wrote the filename. Reformat the file to match.
- **A hand-narrowed attribute it merges.** `visibility` is a merged attribute and
  generated rules carry `//visibility:public`, so a target restricted to
  `["//myapp:__subpackages__"]` comes back public on every run. Pin it with
  `# keep`:

  ```python
  ts_compile(
      name = "internal",
      srcs = ["index.ts"],
      # keep
      visibility = ["//myapp:__subpackages__"],
  )
  ```

  `# keep` is Gazelle's own directive, not a `ts_*` one: above an attribute it
  means "never touch this value", above a whole rule "never touch this rule".
  Without it, a visibility used as an architectural boundary widens back one run
  at a time.

### Which targets carry an alias

A `ts_compile`, the `_doc` compile and the `ts_test` each get `path_aliases` for
the aliases their own srcs import through, plus -- for aliases a `ts_path_alias`
directive declares -- the ones whose directory holds one of those srcs, which is
how a directive-declared alias reaches the IDE tsconfig. The test files are a
program of their own, so the package target's map reaches nothing they compile:
an alias a test imports through is on the `ts_test`.

`ts_compile` accepts an alias only when a file the target stages sits under the
alias directory. A target with a src under it validates the alias on that src,
and the aliased declarations arrive on the dep edge. A target with none gets
`path_alias_srcs` naming the target each aliased import resolved to, filled in at
resolve time the way `deps` is, so that target's outputs -- the declarations in
the bazel-bin mirror of the directory -- are staged. The two shapes in one
package:

```python
ts_test(
    name = "web_test",
    srcs = ["shared/flags.test.ts"],  # under web/shared/: validates the alias
    path_aliases = {"#shared/": "web/shared/"},
    deps = [":web", "@npm//:vitest"],
)

ts_test(
    name = "tooling_test",
    srcs = ["plugins/prerender.test.ts"],  # nothing under web/shared/
    path_alias_srcs = [":web"],
    path_aliases = {"#shared/": "web/shared/"},
    deps = [":web", "@npm//:vitest"],
)
```

Naming the target where a src already validates the alias would stage every
output of that target for nothing, which is why the attribute follows the srcs
rather than the alias.

### Fallback chains in `compilerOptions.paths`

`paths` values are arrays: TypeScript tries each entry in turn. A generated
`path_aliases` attribute holds one directory per alias. Gazelle discards
entries under the `bazel-*` convenience symlinks (`ts_compile` fails analysis on
an alias pointing into the output tree) and entries under a tool-managed
dot-directory such as `.bazel/npm`, then takes the first of what is left that
exists on disk. When none exists on disk (an alias whose directory only a codegen
action produces), the first one is used, silently. That reads the filesystem, so a
chain listing a codegen-produced directory ahead of a checked-in one can resolve
differently on a fresh clone than on a built tree; name one directory per alias
where that matters.

Two cases log, each on a single line (wrapped here to fit):

```
gazelle: typescript: paths entry "@acme/ui/*" resolves on disk to 2 directories;
using "./src/ui/*" and ignoring [./generated/ui/*]. Gazelle emits one directory
per alias; if imports must resolve through more than one, split the alias or list
the extra files in path_alias_srcs.
```

Specifiers that only resolve through the ignored directory get no dep edge, and
the `tsconfig.json` `ts_compile` generates will not carry it either. Setting
`module_name` on the target producing them is the third option.

```
gazelle: typescript: paths entry "@acme/ui/*" has no target Gazelle can use
([./bazel-bin/ui/*]); no path_alias emitted. An alias under bazel-out/bazel-bin
points into the output tree: set module_name on the target that produces those
declarations and import it by that name instead.
```

Every entry pointed into the output tree, so no alias is emitted. An alias with
even one tool-managed dot-directory entry is dropped without this line: that is
the shape `ts_refresh_tsconfig` writes for every npm package, and it is meant to
be dropped.

## Package Boundary Heuristic

By default (**every-dir mode**), every directory that contains `.ts` or `.tsx` source files gets a `ts_compile` target. This matches Go's behaviour where every directory with `.go` files is a package.

**every-dir mode** (default): a directory becomes a boundary when it has any `.ts` files.

**tsconfig mode** (`# gazelle:ts_package_boundary tsconfig`): a directory becomes a boundary when:

1. It holds a `tsconfig.json`, or
2. It has the `# gazelle:ts_package_boundary true` directive, or
3. It is the repository root.

Everything below a boundary that is none of those three rolls up into it, so one
Bazel target covers one TypeScript project. That is the only way to express a
project whose ambient declaration and its sources sit in different directories,
or whose directories import each other — both legal in a single `tsc` program,
and a cycle once every directory is a target of its own.

Test files (`*.test.ts`, `*.spec.ts`, `*.test.tsx`, `*.spec.tsx`) generate `ts_test` targets automatically in both modes.

Doc and story files (`*.doc.ts`, `*.doc.tsx`, `*.stories.ts`, `*.stories.tsx`) generate a separate `ts_compile` target in both modes, for the same reason: a doc file consumes the library rather than belonging to it. Left in the package target, a design system where `switch/switch.doc.tsx` imports `../label` and `label/label.doc.tsx` imports `../switch` is a dependency cycle between the two component packages, even though neither component depends on the other. Like test files, they are outside the `ts_lint` target's sources, and like the `ts_test` target they also get the package's ambient `.d.ts` files: nothing imports an ambient declaration, so only `srcs` membership puts it in a program. `.mdx` files are not TypeScript sources and are unaffected.

### Import Cycles Between Packages

One target per directory means a mutual import between two directories is a
dependency cycle between two Bazel targets. Bazel rejects it with a loop of
target labels when it loads them; Gazelle says the same thing as the BUILD
files are written, and says it about packages rather than labels.

The check runs once, after the last import resolves, over the targets this
extension generates. Each strongly connected component is one message naming
the packages in the cycle and the targets that make it up. That is the whole
of the message: which packages, and that their targets are a cycle Bazel
rejects. A type-only cycle is reported too, though `tsc` accepts one: each
target type-checks its own sources in its own compiler invocation, under
`noEmitOnError`, and an `import type` that resolves to nothing there is a hard
`TS2307`. Not the emit -- a declaration file writes the specifier through
verbatim and never reads what is on the other end.

The message names no import and offers no remedy, and both were tried. Naming
the import behind an edge is the one thing Bazel's own error cannot do, but
every phrasing of it also claims something about which imports carry the cycle
and what removing one would achieve -- and two things falsify such a claim.
The first is an attribute whose computed value never reaches the BUILD file,
whether a `# keep` holds it or the merger cannot reconcile the expression
already there: on `srcs` the file the report would name is not one the target
compiles, and on `deps` an import *between two of the named packages* can drop
out of the edge list, so a value import runs between them while every listed
edge is `import type`. The second is a held `deps` list that agrees with the
imports, where deleting the import does not delete the label. A report that
cannot be wrong is worth more than one that is usually right, so the message
stops at the component.

Nothing is resolved automatically. Merging the cyclic directories into one
target would be `# gazelle:ts_package_boundary` applied without asking --
different labels, coarser granularity -- so the report is the whole feature.

An edge here is one thing: an import a source of the emitted target writes,
whose resolved label that target's emitted `deps` carry. Both halves are read
off the rule Bazel will load wherever that differs from the one Gazelle
computed, and each rules out a case on its own:

- **A `srcs` or `deps` attribute Gazelle's value never reaches** -- held with
  `# keep`, the whole attribute or the whole rule, or holding an expression the
  merger cannot reconcile value by value, which it logs and then leaves
  untouched -- keeps what Gazelle computed out of the BUILD file. An import
  whose label such a `deps` leaves out is therefore not an edge, and neither is
  one written in a file such a `srcs` leaves out. A `# keep` on one `deps`
  *value* is not that: it holds that value and the resolved labels still merge
  in beside it, so a cycle they close is reported as any other.
- **A dep no import explains** -- written by hand, or named by a held `deps`
  the sources do not import -- is not an edge either. Bazel still rejects the
  cycle it closes; the report stays out of it, because what it says is a claim
  about imports and there is no import to make it about. The label is written in
  a BUILD file, which is where Bazel's own loop of labels sends you.

So a cycle whose last edge is hand-written goes unreported, and so does one a
`srcs` or `deps` Gazelle's value never reaches keeps out of the emitted files. What is left is the
cycles the imports and the emitted rules agree on. Where a held `deps` agrees
with the imports the cycle *is* reported, and then the held list is part of what
closes it: removing the import is not enough, because the label stays where you
wrote it.

Only a cycle that crosses package boundaries is reported here. Of the cycles
*inside* one directory, the framework entry split is the one that is reported,
by the framework-entry check, which can name the `entry_point` that split the
target in two. The doc-target and test-target splits put two targets in one
directory too, and a cycle between one of those and the library -- `thing.ts`
importing `./thing.doc` while `thing.doc.tsx` imports `./thing` -- is emitted
with nothing printed and left for Bazel to reject.

A `ts_test` target is one more gap, for the edge rule's reason rather than the
boundary's: it resolves deps from its package's production and doc sources as
well as its own, since it builds its own `node_modules` tree and needs every
npm package the code under test needs. So it can carry a label no source of its
own imports, and a cycle running through such a label is not reported.

## Generated Target Names

| Rule | Name |
|------|------|
| `ts_compile` | directory basename (`src/components` → `components`), `root` at the repository root, or `# gazelle:ts_target_name` |
| `ts_test` | `<ts_compile name>_test` |
| `ts_compile` (docs and stories) | `<ts_compile name>_doc` |
| `ts_lint` | `<ts_compile name>_lint` |
| `ts_dev_server` | `dev` |
| `css_library`, `css_module`, `asset_library`, `json_library` | the source filename with `.` replaced by `_` |

Non-TypeScript libraries keep the extension in the name — `button.css` →
`button_css`, `logo.svg` → `logo_svg`, `config.json` → `config_json`,
`Button.module.css` → `Button_module_css`. That keeps the directory-named
`ts_compile` target free (a `components/` directory holding `components.css`
would otherwise generate two targets named `components`) and keeps files that
share a stem apart (`logo.svg` and `logo.json`). A tie that survives both gets a
numeric suffix on the later name (`_2`).

The generated `ts_dev_server` gets `plugin` set and no `server`, so it runs the
default Vite implementation. Gazelle writes the rule only when the package has
no `dev` target yet, so a hand-added
`server = "@rules_typescript//oj:dev_server"` survives later runs. See
[Choosing the server](../guides/dev-server.md#choosing-the-server).

## The compilerOptions Baseline

Every generated `ts_compile` — the package target and the `_doc` one alike —
and every `ts_test` names the nearest hand-written `tsconfig.json` in its own
directory or an ancestor, so a target compiles under the repo's own `lib`,
`types`, `jsx` and strictness rather than only the ruleset's defaults. A
`ts_config` target beside the file is what makes it a label a subpackage can
name:

```python
# packages/core/BUILD.bazel
ts_config(
    name = "tsconfig",
    src = "tsconfig.json",
    visibility = ["//visibility:public"],
)

# packages/core/src/BUILD.bazel
ts_compile(
    name = "src",
    srcs = ["index.ts"],
    tsconfig = "//packages/core:tsconfig",
    visibility = ["//visibility:public"],
)
```

The `ts_config` goes into the directory holding the file even when nothing else
there is a target: the pnpm workspace-member layout — `package.json` and
`tsconfig.json` beside each other with the sources under `src/` — is exactly
that shape, and without a BUILD file there the label above names a target in a
package Bazel never loads, which fails analysis for the whole workspace.

Naming a tsconfig **adds** its options and never removes the ruleset's own. The
four the rule supplies — `strict`, `module: Preserve`, `skipLibCheck`,
`esModuleInterop` — apply with a `tsconfig` too, under it, so running Gazelle
over a working build does not silently un-set them. `moduleResolution` is left
for tsgo to derive from whichever `module` wins, since a value under a
`tsconfig` that sets `module` would be the wrong half of a pair. See
[where compiler options come from](../rules/ts-compile.md#where-compiler-options-come-from).

Three cases get no attribute rather than a label into a directory Gazelle writes
no BUILD file into, each logged with the fix. The label is resolved once per
package, so a refusal reaches every target there — the `_doc` one included:

- a directory under a `# gazelle:ts_ignore`, and one inside a tree Next.js or
  SvelteKit stages by glob;
- one a `# gazelle:ts_package_boundary` directive *between* it and the naming
  package leaves the two disagreeing about — the mode inherited by the second
  says nothing about the first, and a guess either way is a label nothing writes;
- a directory whose own target is already named `tsconfig` or
  `tsconfig_types`, the two names the `ts_config` and the filegroup below need.

A tree with no `tsconfig.json` above it keeps the ruleset baseline alone. The
`tsconfig.json` files `ts_refresh_tsconfig` writes are skipped: they are built
out of the very targets that would name them.

Starlark cannot read a tsconfig to follow its `extends` chain, so a file that
extends another needs that chain in the generated `ts_config`'s `deps`. Gazelle
writes it for the one shape it can read without guessing: a single relative
specifier naming an ancestor directory's own `tsconfig.json`, which is what a
per-directory tsconfig split produces.

```python
# workers/proxy/test/BUILD.bazel — its tsconfig.json is {"extends": "../tsconfig.json"}
ts_config(
    name = "tsconfig",
    src = "tsconfig.json",
    deps = ["//workers/proxy:tsconfig"],
    visibility = ["//visibility:public"],
)
```

Without that dep the base is not an input to any action, so tsgo reads the path
out of the config, finds nothing at it and reports `TS5083: Cannot read file`
before it reaches a question about the sources.

Every other shape is the author's to declare. An `extends` array states a merge
order and not which entry to stage; a package-form specifier resolves through
node_modules; an absolute path resolves on the one machine that wrote it. So
does a base with no label to name it — outside the repository, at a path no file
sits at, or in a directory that generates no `ts_config` for one of the three
reasons listed above, which is logged where it applies.

`deps` is Gazelle's, recomputed on every run, which is what lets a run correct
the label it wrote when the base moves or goes away — a `deps` Gazelle could not
revisit would leave a label no target satisfies and fail analysis for the whole
workspace. So a hand-written value needs a `# keep` on its line to survive the
next run, as does a hand-picked `tsconfig` on the compile, doc and test targets.

### A declaration the tsconfig names

One key does not survive being inherited: a relative `compilerOptions.types`
entry. The generated per-directory config states its own `files`, `include` and
`exclude` and takes `compilerOptions` from the project file through `extends` —
and TypeScript resolves `./x.d.ts` against the config the program was invoked
with, which is the generated one in `bazel-out`. So
`"types": ["./worker-configuration.d.ts"]` — how wrangler writes it — reaches
nothing from a directory below, and every global that file declares is
`TS2304`.

Gazelle rebases the entry onto the four kinds it generates under the tsconfig
that type-check — the package `ts_compile`, the framework client entry, the
`_doc` compile and the `ts_test` — and names the file by a label. A
`ts_bundle`, a `ts_dev_server`, a `node_modules` or the `ts_config` itself has
no type program, and gets neither the entry nor the label:

```python
# workers/proxy/BUILD.bazel
filegroup(
    name = "tsconfig_types",
    srcs = ["worker-configuration.d.ts"],
    visibility = ["//visibility:public"],
)

# workers/proxy/src/BUILD.bazel
ts_compile(
    name = "src",
    srcs = ["handler.ts"],
    tsconfig = "//workers/proxy:tsconfig",
    types = ["../worker-configuration.d.ts"],
    types_srcs = ["//workers/proxy:tsconfig_types"],
    visibility = ["//visibility:public"],
)
```

A generated `ts_test` carries the same pair, forwarded to the `ts_compile` it
makes for the test sources — see
[`ts_test`'s `types_srcs`](../rules/ts-test.md#a-types-entry-that-names-a-declaration-file).

A `filegroup`, so the file is an action input of exactly the targets that name
it and of nothing else. It reaches no consumer's program: `types_srcs` travels
on no dep edge, and nothing here names the file in `public_globals`, which is
what would put the declaration in every transitive consumer — see
[which ambients a consumer gets](../rules/ts-compile.md#which-ambients-a-consumer-gets).

A file that a [`ts_worker_types`](../rules/ts-worker-types.md) target in the
tsconfig's own BUILD file writes is not in the source tree, so no filegroup is
written for it: the target is the label. Gazelle reads the target's `out`
(default `worker-configuration.d.ts`) to pair it with the entry.

```python
# workers/proxy/BUILD.bazel -- hand-written; Gazelle leaves it in place
ts_worker_types(
    name = "worker_types",
    config = "wrangler.jsonc",
    node_modules = ":node_modules",
)

# workers/proxy/src/BUILD.bazel
ts_compile(
    name = "src",
    srcs = ["handler.ts"],
    tsconfig = "//workers/proxy:tsconfig",
    types = ["../worker-configuration.d.ts"],
    types_srcs = ["//workers/proxy:worker_types"],
    visibility = ["//visibility:public"],
)
```

The whole `types` list is written, not just the file entries: `types` is one
key and `extends` replaces it whole, so a target carrying a subset would drop
the packages the project asked for. Whether those resolve is unchanged — a
package entry is answered from `deps`, which the `ts_ambient_types` reading of
the same key already supplies.

`compilerOptions.types` is the only key read for this. A declaration named in
`include` gets nothing, because `include` does not survive `extends` into the
generated config — it states its own — so it makes no claim about the tree
below it. Two shapes are logged and produce nothing rather than a guess: an
entry naming a path outside the tsconfig's own directory, which no label of
that package can stage, and one naming a file that is neither there nor written
by a `ts_worker_types` target beside the tsconfig.

## Automatic Lint Targets

When a linter config file is present in the current directory or any ancestor, and `pnpm-lock.yaml` has the linter it is for, Gazelle generates a `ts_lint` target alongside each `ts_compile` target. The lint target name is the compile target name with `_lint` appended. `linter_binary` is the hub's bin alias for the linter package — `@npm//:eslint_bin`, or `@npm_eslint//:eslint_bin` in a tree under `# gazelle:ts_npm_hub npm_eslint`.

Detected config files:
- **oxlint**: `oxlint.json`, `.oxlintrc.json`, `.oxlintrc`
- **eslint**: `eslint.config.mjs`, `eslint.config.js`, `eslint.config.cjs`, `.eslintrc.json`, `.eslintrc.*`

oxlint configs are detected before ESLint configs. The closest config file wins.

A config on disk whose linter the lockfile never mentions — the `eslint.config.js` of a nested `package-lock.json` island, or one left behind by a package that was never installed — gets no `ts_lint`. Its binary would be a target the hub does not declare, and Bazel answers `no such target` by failing analysis for every target in the package, not the lint alone. Gazelle says so once per config file, naming the config, the package, and the label, and withdraws a `ts_lint` an earlier run wrote for it; the fix is to add the linter to the workspace's dependencies or to delete the config. A tree under its own `# gazelle:ts_npm_hub` resolves against a lockfile Gazelle never read, so nothing there is refused, and a workspace with no root lockfile is not refused either.

Example generated output with an `oxlint.json` at the repo root:

```python
ts_compile(
    name = "my_lib",
    srcs = ["index.ts"],
    visibility = ["//visibility:public"],
)

ts_lint(
    name = "my_lib_lint",
    srcs = ["index.ts"],
    linter = "oxlint",
    linter_binary = "@npm//:oxlint_bin",
    config = "//:oxlint.json",
)
```

To run linting:

```bash
bazel build //... --output_groups=+_validation
```

## Configuration

Gazelle reads `compilerOptions.paths` and `compilerOptions.baseUrl` straight from
the nearest `tsconfig.json`, parsed as JSONC: comments and trailing commas are
accepted. Everything else is configured with `# gazelle:ts_*` directives in BUILD
files; see the [Directives Reference](directives.md).

`extends` is followed, written either as one specifier or as an array of them,
and merged the way `tsc` merges it: the config nearest the leaf wins a key
outright rather than merging into it, so a `paths` map always comes from exactly
one file in the chain, and its relative targets are resolved against the
directory of the config that wrote them. A specifier that resolves through
`node_modules` (`"@tsconfig/node20/tsconfig.json"`) is skipped with a warning —
Gazelle reads only configs on disk — so inline the options such a config carries
or extend a checked-in copy instead.

Directives take precedence over file-based configuration, and a directory's
`ts_path_alias` directives merge with whatever aliases reached it: a child adds
keys and overrides one key at a time. A `tsconfig.json` with `paths` does not
merge. It replaces the alias map for its directory and everything below, parent
directives included, and the directives in its own BUILD file then merge on top.

### Runtime deps of generated tests

`# gazelle:ts_runtime_dep` lists Bazel labels appended to every generated `ts_test` deps list. Use this for packages needed at test runtime but never statically imported:

| Package | Why it needs to be explicit |
|---------|----------------------------|
| `@npm//:happy-dom` | vitest environment — imported by vitest config, not your test files |
| `@npm//:react` | JSX runtime (`react/jsx-runtime`) — never directly imported |
| `@npm//:react-dom` | required for React test utilities |
| `@npm//:types_react` | type declarations for JSX |

## Framework Detection

When the workspace-root `package.json` names a framework Gazelle recognises, the
root BUILD file gets that framework's bundle wiring: a `node_modules` tree, a
`vite_bundler`, and a `ts_bundle` with `staging_srcs`, `vite_config` and
`entry_point` already set. Detection is by dependency name, in `dependencies` or
`devDependencies`, so there is nothing to configure.

Recognising a framework and being able to bundle it are two different things:

| `package.json` names | Gazelle emits |
|---|---|
| `@tanstack/react-router`, `@tanstack/start` | the Vite bundle targets |
| `@remix-run/dev`, `@remix-run/react` | the Vite bundle targets |
| `next` | `node_modules` + `next_build` + `next_dev_server` — its own rules, not Vite |
| `@sveltejs/kit` | `node_modules` + `sveltekit_build` — its own rule, not Vite |
| `@solidjs/start`, `solid-start` | nothing, plus a message saying why |

For the last one no BUILD file closes the gap, and a generated `ts_bundle` would
fail `bazel build //...`. Gazelle writes no bundle target and logs the framework,
the reason, and the fallback:

```
typescript: SolidStart detected: bundling it is unsupported, so no bundle target
was generated — @solidjs/start ships no Vite plugin: defineConfig() returns a
vinxi app, which ts_bundle's vite_config contract (a default export with a
plugins array) cannot consume. Your TypeScript still compiles and tests; for a
client-only build, declare a ts_bundle by hand with no vite_config.
```

SvelteKit is off the `ts_bundle` path for a reason of the same kind. Its plugin
runs SvelteKit's own sync step from the Vite `config` hook, which wants a
`src/app.html` and a `svelte.config.js` of its own beside the Vite config, and it
reads the route tree off `process.cwd()`. `sveltekit_build` owns that instead: it
globs `src/` and the assets tree, and TypeScript outside them reaches the build
through `staging_srcs`.

### Solid Start

`@solidjs/start`'s `./config` export has one symbol, `defineConfig`, and the vinxi
app it returns has no `plugins` array: vinxi owns the server, the router manifest
and the build. `ts_bundle`'s `vite_config` contract is a default export whose
`plugins` are prepended to Bazel's, and `unhandled_keys_js` rejects a
`vite_config` whose own keys are not a subset of `plugins` and `root`. A vinxi app
is nothing but other keys, so a generated target fails to build. Solid Start is
registered in `unsupportedBundling` and not as a `frameworkConfigs` entry.

Two changes would each reopen it, and neither is small:

- **`@solidjs/start` ships a Vite plugin.** Upstream's call. Solid Start then
  joins TanStack Start and Remix on the existing path with no new rule code: a
  three-line `solid-vite.config.mjs` naming the plugin, a `frameworkConfigs`
  entry for the npm deps, stage dirs and client entry, and the refusal deleted.
- **A `BundlerInfo` implementation drives vinxi.** `ts_bundle` takes any bundler
  returning [`BundlerInfo`](../guides/bundling.md#custom-bundler-bundlerinfo-interface),
  so a rule wrapping vinxi's build as the bundler binary sidesteps the
  `vite_config` contract. It is the larger change: vinxi's route manifest, server
  output and multi-target build have no counterpart in either `BundlerInfo`
  invocation mode.

`solid-js` with `vite-plugin-solid` is an ordinary Vite plugin and goes through
`vite_config` like any other. Detection matches only `@solidjs/start` and
`solid-start`, so a plain `solid-js` workspace never reaches the unsupported
path; no test in this repository covers that combination.

!!! note "Documented from the refusal, not from an install"

    `@solidjs/start` is in no `package.json` or lockfile here. The shape of
    `defineConfig`'s return value above comes from the refusal string in
    `gazelle/framework_bundle.go` and the package's published API. Confirm against
    the installed package before acting on it.

### The Entry Point Is Generated

`ts_bundle` takes exactly one `.js` as its entry, and Gazelle merges every source
in a directory into one target, so the framework's conventional client entry needs
a target of its own. Gazelle writes it: it recognises the file the `entry_point`
label names, gives it a single-file `ts_compile`, and leaves it out of the
directory-wide one.

```python
# app/BUILD.bazel — generated
ts_compile(
    name = "entry_client",
    srcs = ["entry.client.tsx"],
    visibility = ["//visibility:public"],
)

ts_compile(
    name = "app",
    srcs = ["root.tsx"],
    visibility = ["//visibility:public"],
)
```

Nothing to declare, and nothing to exclude. The pre-0.2 recipe (a
`# gazelle:ts_exclude` on the entry file plus a hand-written `ts_compile`) still
works, but Gazelle maintains neither half of it: the exclusion drops the file
before the generator sees it. The run reports it:

```
typescript: Remix detected: a ts_exclude directive drops app/entry.client.tsx,
the bundle's client entry, so Gazelle generates no "entry_client" target and does
not maintain the one you wrote in its place -- an import added to the entry never
reaches its deps, and ts_compile's strict-deps check fails on that import. Drop
the directive and the hand-written target: Gazelle writes the single-file entry
target itself now.
```

When nothing in that package maps to the entry name, no bundle target is generated
either: `entry_point` would name nothing, and a dangling label fails
`bazel build //...` for the whole workspace. That covers both a missing file and
one an `exclude` drops. `//tests/integration:remix_test` pins the workspace-wide
failure and the generated entry target;
`TestFrameworkEntry_BuiltinExcludeAndTsIgnoreLeaveNoDanglingLabel` pins the
skipped bundle.

## Import Resolution

Gazelle resolves TypeScript imports to Bazel labels in this order:

1. **Relative imports** (`./foo`, `../bar`) — resolved to the `ts_compile` target in that directory
2. **Path aliases** — from `compilerOptions.paths` in the nearest `tsconfig.json`, the `imports` field of the nearest `package.json`, or a `# gazelle:ts_path_alias` directive
3. **A first-party `module_name`** — a bare specifier is matched against the `module_name` of the indexed `ts_compile` targets before npm is considered, because the `@npm` hub has no package under that name
4. **npm packages** — resolved to `@npm//:<label>` using the pnpm lockfile
5. **Unresolved** — optionally warned with `# gazelle:ts_warn_unresolved true`

A specifier that spells out an extension resolves like one that does not.
`./rules/foo.js`, `./rules/foo.ts` and `./rules/foo` are matched against one
candidate list: the path as written, the path with its extension dropped, that
stem under each known extension, and `<stem>/index.ts[x]`. NodeNext-style `.js`
specifiers over `.ts` sources therefore resolve to the target that owns the
source.

A `#`-prefixed specifier is a Node package-private import, answered only by the
`imports` field of the package's own `package.json`:

```json
{ "imports": { "#shared/*": "./shared/*" } }
```

Gazelle reads that map as a path alias, so `#shared/flags` resolves to the
target owning `<pkg>/shared/flags`. A conditions object or an array picks one
target (`types`, then `import`, `module`, `default`, `node`, `require`). An
entry a `paths` key already covers keeps the `paths` answer, and an inner
package's map replaces an outer package's answer for the same key — Node
answers a `#` from the nearest enclosing `package.json`.

A target may name another package instead of a path, which is how the field
swaps a polyfill by condition:

```json
{ "imports": { "#dep": { "node": "./src/node.ts", "default": "lodash" } } }
```

That entry resolves to `@npm//:lodash`, subpaths and a trailing `/*` wildcard
included. A `#` specifier no entry covers resolves to nothing rather than to an
npm label — there is no npm package of that name. Node allows `*` anywhere in a
pattern, but an alias key matches by prefix: `#internal/*/utils` is dropped
rather than recorded as a key that could never fire.

Node built-ins resolve to `@types/node`, with or without the `node:` prefix:
`import "path"` and `import "node:path"` both take the declarations dep, since
Node supplies the module at runtime but nothing supplies its types. A package
installed under a built-in's name (the browserify `path` shim, say) still wins.
When the lockfile has no `@types/node` the import gets no dep at all — a label
no hub declares would turn a type error into an analysis failure.

### The npm inventory

The names in step 4 come from the workspace-root `pnpm-lock.yaml`, read once per
Gazelle run. The inventory is what the `@npm` hub declares a flat `//:<label>`
for, which is the whole resolved closure and not only what a `package.json`
lists: `npm/lazy.bzl` gives every package in the lockfile a label, so a
transitive `@types/node` is as real a dep target as a direct one.

Two bounds on that, both deliberate:

- A package built for specific platforms (`os:`, `cpu:`, `libc:` — the native
  sidecars like `@esbuild/linux-x64` and `fsevents`) is left out. Matching the
  platform table exactly would mean a second copy of it in Go, and no
  TypeScript source imports those by name.
- Only lockfile format 6.x and 9.x are read, the same two
  `npm/private/npm_translate_lock.bzl` reads. Any other version logs a warning
  and leaves the inventory absent, which is not the same as empty: everything
  gated on the inventory (the `@types/node` dep, the codegen detectors, the
  framework bundle's npm deps) falls back to file-presence heuristics rather
  than concluding the workspace declares nothing.

A repo with no lockfile is in that same absent state, which is why the codegen
detectors emit a target from a `schema.prisma` alone there and check the
dependency where a lockfile exists.

### A name the lockfile never mentions

Step 4 stops short of the label when the lockfile has never heard of the name at
all. The hub is built from that lockfile, so `@npm//:anthropic-ai_sdk` for a
package a nested `package-lock.json` installed, or `@npm//:_integrations` for a
`@/integrations/...` alias no `tsconfig` in scope expands, is a target that
cannot exist — and `no such target` fails analysis for every target in the
build, where a missing dep fails the one import that needed it with `TS2307`.
`# gazelle:ts_warn_unresolved true` lists them.

This reads a wider set than the inventory above, on purpose. The inventory
under-claims (the platform-restricted packages), and refusing a label on an
under-claim would drop a real dep, so the refusal takes every name either
section of the lockfile spells plus every workspace link and npm alias. Two
things are never refused: a tree carrying its own `# gazelle:ts_npm_hub`, which
resolves against a second lockfile nobody read here, and a workspace with no
root lockfile at all.

When several alias entries match one specifier (a tsconfig declaring both
`"@shared"` and `"@shared/*"`), the longest matching alias key wins, which is
TypeScript's own rule: a pattern equal to the whole specifier is the longest key
that can match it. An alias key without a trailing wildcard matches only at a
path-segment boundary, so `@shared` does not claim `@sharedX`.

An import of an extension no rule here claims (`./notes.rst`) resolves to
nothing at all rather than to a label under the file's own name: `//pkg/notes.rst`
is a package Bazel cannot load, and a missing package fails every target in the
build instead of the one that lost a dep. A leading dot belongs to the name and
is not an extension, so `./tools/.internal` is read as the directory it is and
goes on to the checks below; `./tools/.internal.old` still reads as a file.

A specifier that maps to a directory that is not on disk resolves to nothing for
the same reason. `#shared/i18n/compiled/messages` under an
`"#shared/*": "./shared/*"` entry names `//web/shared/i18n/compiled/messages`,
and if nobody has generated that directory Bazel answers `no such package`
before any compile runs. Dropping the dep leaves the one `TS2307` the missing
module deserves.

A specifier that lands in a directory the generator will never write a BUILD
file in resolves to nothing for the same reason. Two rules decide that, and the
resolver reads both: `skipRolledUpDir` skips a dot-directory, `node_modules`,
`dist` and `bazel-out`, and under `tsconfig` mode a directory that is not a
boundary of its own is rolled into the package above it. The mode is the one
declared at or above the directory the specifier lands in, not the one the
importer is generated under, so a tree that puts `tsconfig` over one subtree and
`every-dir` over another keeps the deps that cross between them. So
`../../shared/public/.well-known/assetlinks.json?raw` names
`//web/shared/public/.well-known`, and under
`# gazelle:ts_package_boundary tsconfig` a `./preview.css` written in a
rolled-up `scripts/preview.ts` names `//pkg/scripts` — both packages the
generator has already decided will not exist. An indexed rule in such a
directory still answers first, so a dot-directory package generated in
`every-dir` mode is unaffected, and so is one whose `BUILD` file is checked in
by hand: that file is the proof Bazel can load the package, and what the
generator would or would not write there says nothing about it.

A bare specifier a `declare module "x"` block in the target's own sources names
resolves to nothing, before the npm step. In a script-mode declaration file such
a block is the module -- `declare module "mobile"` beside the code importing
`"mobile"` -- so no dep can carry it and `@npm//:mobile` would name a target no
hub declares. Only the target holding the declaration is exempt, and an
installed package of the same name keeps its dep: the lockfile is the claim that
a hub target exists. A pattern name (`declare module "*.svg"`) is ignored, since
the specifiers it covers are relative paths with real targets.

Gazelle's deps and the `ts_compile` strict-deps check share one specifier
scanner. If `bazel build` reports an import no direct dep provides and re-running
Gazelle does not add it, that is a bug in the ruleset.

See [Directives Reference](directives.md) for all available directives.
