# Gazelle Overview

Gazelle auto-generates BUILD files from TypeScript source files, inferring `ts_compile` targets and resolving imports to Bazel labels.

## Setup

Add the Gazelle binary to your root `BUILD.bazel`:

```python
load("@gazelle//:def.bzl", "gazelle")

gazelle(
    name = "gazelle",
    gazelle = "@rules_typescript//gazelle:gazelle_ts",
)
```

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

**index-only mode** (`# gazelle:ts_package_boundary index-only`): a directory becomes a boundary when:

1. It contains an `index.ts` or `index.tsx` file, or
2. It has the `# gazelle:ts_package_boundary true` directive, or
3. It is the repository root.

!!! note "Upgrading from pre-0.2.0"
    Earlier versions used **index-only mode** by default. If you relied on that behaviour, add `# gazelle:ts_package_boundary index-only` to your root `BUILD.bazel` to restore it.

Test files (`*.test.ts`, `*.spec.ts`, `*.test.tsx`, `*.spec.tsx`) generate `ts_test` targets automatically in both modes.

## Generated Target Names

| Rule | Name |
|------|------|
| `ts_compile` | directory basename (`src/components` → `components`), `root` at the repository root, or `# gazelle:ts_target_name` |
| `ts_test` | `<ts_compile name>_test` |
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

## Automatic Lint Targets

When a linter config file is present in the current directory or any ancestor, Gazelle automatically generates a `ts_lint` target alongside each `ts_compile` target. The lint target name is the compile target name with `_lint` appended.

Detected config files:
- **oxlint**: `oxlint.json`, `.oxlintrc.json`, `.oxlintrc`
- **eslint**: `eslint.config.mjs`, `eslint.config.js`, `eslint.config.cjs`, `.eslintrc.json`, `.eslintrc.*`

oxlint configs are detected before ESLint configs. The closest config file wins.

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

Directives take precedence over file-based configuration, and a directory's
`ts_path_alias` directives merge with whatever aliases reached it: a child adds
keys and overrides one key at a time. A `tsconfig.json` with `paths` does not
merge. It replaces the alias map for its directory and everything below, parent
directives included, and the directives in its own BUILD file then merge on top.

### gazelle_ts.json (deprecated)

A `gazelle_ts.json` in a directory is still read. Gazelle prints a deprecation
warning naming the directive that replaces each key:

| Key | Replacement |
|---|---|
| `pathAliases` | `# gazelle:ts_path_alias @/ src/` |
| `excludePatterns` | `# gazelle:ts_exclude *.generated.ts` |
| `runtimeDeps.test` | `# gazelle:ts_runtime_dep @npm//:happy-dom` |
| `excludeDirs` | no directive; excluded directories are the built-in set plus this key |
| `npmMappingFile` | no directive; a JSON file mapping npm names to labels |

It sits above `tsconfig.json` and below directives in precedence. Only the two
keys with no directive replacement keep it alive; do not add new uses.

`runtimeDeps.test` (or `# gazelle:ts_runtime_dep`) lists Bazel labels appended to every generated `ts_test` deps list. Use this for packages needed at test runtime but never statically imported:

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
2. **Path aliases** — from `compilerOptions.paths` in the nearest `tsconfig.json`, or a `# gazelle:ts_path_alias` directive
3. **A first-party `module_name`** — a bare specifier is matched against the `module_name` of the indexed `ts_compile` targets before npm is considered, because the `@npm` hub has no package under that name
4. **npm packages** — resolved to `@npm//:<label>` using the pnpm lockfile
5. **Unresolved** — optionally warned with `# gazelle:ts_warn_unresolved true`

A specifier that spells out an extension resolves like one that does not.
`./rules/foo.js`, `./rules/foo.ts` and `./rules/foo` are matched against one
candidate list: the path as written, the path with its extension dropped, that
stem under each known extension, and `<stem>/index.ts[x]`. NodeNext-style `.js`
specifiers over `.ts` sources therefore resolve to the target that owns the
source.

Node built-ins get no dep, with or without the `node:` prefix: `import "path"`
and `import "node:path"` are both left alone, matching what the strict-deps check
exempts.

When several alias entries match one specifier (a tsconfig declaring both
`"@shared"` and `"@shared/*"`), the longest matching alias key wins, which is
TypeScript's own rule: a pattern equal to the whole specifier is the longest key
that can match it. An alias key without a trailing wildcard matches only at a
path-segment boundary, so `@shared` does not claim `@sharedX`.

Gazelle's deps and the `ts_compile` strict-deps check share one specifier
scanner. If `bazel build` reports an import no direct dep provides and re-running
Gazelle does not add it, that is a bug in the ruleset.

See [Directives Reference](directives.md) for all available directives.
