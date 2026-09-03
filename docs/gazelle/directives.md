# Gazelle Directives Reference

Directives go in `BUILD.bazel` files as comments and control how Gazelle generates BUILD rules for that directory and its children.

## Full Reference

| Directive | Effect |
|-----------|--------|
| `# gazelle:ts_declarations oxc` | Emit `declarations = "oxc"` on generated `ts_compile` and `ts_test` rules in this tree — syntactic `.d.ts` emit, so every export needs an explicit type |
| `# gazelle:ts_declarations tsgo` | Return a subdirectory to the default emitter after a parent set `oxc` |
| `# gazelle:ts_package_boundary every-dir` | (default) Every directory with `.ts` files becomes a package |
| `# gazelle:ts_package_boundary tsconfig` | Only directories holding a `tsconfig.json` become packages, so one target covers one TypeScript project |
| `# gazelle:ts_package_boundary true` | Mark this single directory as a boundary (in `tsconfig` mode, where the covering `tsconfig.json` sits elsewhere) |
| `# gazelle:ts_ignore` | Suppress TypeScript rule generation for this directory and its children |
| `# gazelle:ts_ignore false` | Re-enable generation after a parent used `ts_ignore` |
| `# gazelle:ts_target_name my_lib` | Override the default target name (which is the directory basename) |
| `# gazelle:ts_path_alias @/ src/` | Map a TypeScript path alias to a workspace-relative directory |
| `# gazelle:ts_runtime_dep @npm//:happy-dom` | Append a label to every generated `ts_test` deps list |
| `# gazelle:ts_ambient_types @npm//:types_node` | Append a label to every generated `ts_compile` and `ts_test` deps list |
| `# gazelle:ts_exclude *.generated.ts` | Exclude files matching this pattern from source targets; a pattern with no path matches that basename at every depth below |
| `# gazelle:ts_exclude ./vite.config.ts` | Exclude one path, resolved against the directory whose build file declares it |
| `# gazelle:ts_exclude_dir coverage` | Keep Gazelle out of every directory with that basename below here; appends to the set an ancestor declared |
| `# gazelle:ts_warn_unresolved true` | Warn when an import cannot be resolved to a Bazel label |
| `# gazelle:ts_codegen <name> <generator> <outs> [srcs:<csv>] [args…]` | Register a `ts_codegen` target in this directory; a `srcs:` entry may be a `glob()` call |
| `# gazelle:ts_npm_hub npm_eslint` | Resolve bare specifiers in this tree into that npm hub repo, not the default `@npm` |
| `# gazelle:ts_npm_mapping npm/mapping.json` | Overlay a workspace-root-relative JSON file of npm name → Bazel label onto the lockfile inventory, per key |
| `# gazelle:ts_asset_declaration_type .svg <type>` | What an import of that asset extension resolves to in this tree, written into every `asset_library`'s `declaration_type` |
| `# gazelle:ts_js_srcs .mjs .cjs` | Admit JavaScript sources of these extensions into the `srcs` Gazelle generates in this tree; named with nothing after it, admit none |

That is the complete set: fifteen directives. Gazelle warns on an unknown
`# gazelle:ts_*` comment and continues, so a typo in a directive's name shows up
in the run output. A value `ts_package_boundary` does not know stops the run
instead: the modes decide which files each target compiles, and a directive that
did nothing would leave the tree compiling to something other than what its
author wrote.

## `# keep`

`# keep` is Gazelle's own comment, understood by every language extension. Above
an attribute it means "never rewrite this value"; above a whole rule, "never
rewrite this rule". Reach for it whenever a run keeps undoing an edit you meant:

```python
ts_compile(
    name = "internal",
    srcs = ["index.ts"],
    # keep
    visibility = ["//myapp:__subpackages__"],
)
```

`visibility` is the common case: it is a merged attribute and every rule Gazelle
generates carries `//visibility:public`, so without `# keep` a hand-narrowed
visibility widens back on every run. See
[getting the clean-tree diff to empty](overview.md#getting-the-clean-tree-diff-to-empty).

### Attributes Gazelle Owns

Gazelle recomputes these from the tree on every run, so a value it cannot derive
is replaced unless a `# keep` holds it. `ts_compile.deps` and
`next_build.staging_srcs` are equally Gazelle's:

| Rule | Attributes Gazelle owns |
|------|-------------------------|
| `ts_compile` | `srcs`, `deps`, `visibility`, `path_aliases`, `declarations`, `tsconfig` |
| `ts_test` | `srcs`, `deps`, `tsconfig` |
| `ts_config` | `src`, `deps`, `visibility` |
| `ts_lint` | `srcs`, `linter`, `linter_binary`, `config`, `fail_on_warnings` |
| `css_library`, `css_module`, `asset_library`, `json_library` | `srcs`, `deps`, `visibility` |
| `asset_library` | `declaration_type`, one entry per extension a `ts_asset_declaration_type` directive names — an extension no directive names is yours |
| `ts_codegen` | `outs`, `out_dir`, `visibility` |
| `next_build` | `srcs` (a `glob()`), `staging_srcs`, `config`, `tsconfig`, `node_modules` |
| `next_dev_server` | `node_modules` |
| `sveltekit_build` | `srcs` (a `glob()`), `staging_srcs`, `config`, `svelte_config`, `node_modules` |
| `ts_bundle` (framework root) | `staging_srcs`, `entry_point`, `html`, `vite_config`, `mode`, `bundler` |
| `vite_bundler` | `vite`, `node_modules` |
| `node_modules` (framework root) | `deps` |
| `filegroup(name = "sources")`, `filegroup(name = "tsconfig_types")` | `srcs`, `visibility` |

`ts_config.deps` is the `extends` chain, and Gazelle writes it from the one
specifier shape it can read without guessing — a single relative path naming an
ancestor directory's own `tsconfig.json`. Every other shape gets no value, which
for an owned attribute means the value goes: a hand-written `deps` needs a
`# keep` on its line to survive the next run. See
[the compilerOptions baseline](overview.md#the-compileroptions-baseline).

`ts_compile.public_globals` is deliberately absent. Whether a `.d.ts`'s globals
are part of the package's public type surface is a decision nothing in the
source states, so no directive writes it and a hand-written value survives every
run, `# keep` or not.

`types` and `types_srcs` are a third case: generated, and not owned. Gazelle
writes both where the nearest `tsconfig.json` names a declaration file of its
own directory in `compilerOptions.types` — see
[a declaration the tsconfig names](overview.md#a-declaration-the-tsconfig-names).
Neither is mergeable, so the value on disk wins whenever there is one, and
`rule.MergeRules` copies in an attribute the rule does not carry at all: which
means **deleting the lines does not opt out** — they come back on the next run.
Two things stick. `types = []` with `types_srcs = []` keeps both attributes
present and asks for no ambient types at all, dropping the package entries the
tsconfig named along with the file. A `# keep` above the whole `ts_compile`
keeps whatever you wrote and leaves the entries where `extends` puts them,
unresolved, which is where they were before Gazelle wrote anything.

Three kinds are the exception: `ts_dev_server`, `ts_pnpm` and `ts_add_package`.
Each is written once, when no rule of that name exists, and left alone from then
on. Gazelle emits no candidate for a rule that already exists, so the merger
never runs on one. `ts_add_package` declares `pnpm_lock` mergeable and no merge
ever reaches it. Their attributes are yours after the first run, `# keep` or not.

`# keep` works at three granularities: one value, one attribute, one rule. All
three write paths honour all three — the merger's; the direct write a `glob()`
needs, since the merger cannot merge a call expression; and the entry-by-entry
merge `path_aliases` needs, since the merger has no case for a dict:

```python
next_build(
    name = "app",
    # A single pattern Gazelle did not derive: an assets tree the framework
    # config points somewhere unconventional.
    srcs = glob([
        "app/**",
        "content/**",  # keep
    ]),
    # A dep no import implies: next/image loads sharp at runtime.
    node_modules = ":node_modules",
    staging_srcs = [
        "//lib",
        "//vendor:vendor_hand",  # keep
    ],
    # keep
    config = "custom.next.config.mjs",
)

ts_compile(
    name = "app",
    srcs = ["main.ts"],
    # One alias entry no import implies: a directory a codegen action writes.
    path_aliases = {
        "@/": "src/",
        "@gen/": "src/generated/",  # keep
    },
)
```

A run that drops a value from one of these attributes reports it:

```
typescript: next_build(app) in BUILD.bazel: Gazelle generates staging_srcs and
recomputed it from the tree, so "//vendor:vendor_hand" is no longer declared. A
value Gazelle cannot derive needs a "# keep" comment on its own line to survive
the next run; "# keep" above the attribute hands the whole attribute back to you.
```

Every dropped value is reported, whether it was a stale label Gazelle wrote
itself or an edit of yours. That report is the whole difference from Gazelle's Go
extension, which drops the same values silently: what survives a run is identical
either way.

One case is deliberately silent: a value whose file or package is no longer on
disk. Deleting a staged directory drops the label that named it, and holding that
label with `# keep` would name a source nothing provides, which fails analysis. A
value whose target is still on disk is always reported.

#### Values Gazelle Cannot Merge

`# keep` decides what survives among plain strings and plain lists of plain
strings, the two shapes Gazelle's merger reconciles value by value. A value in
any other shape it cannot merge at all: a module-level variable, two lists joined
with `+`, a `select()`, or a list with one variable element.

What happens then is Gazelle's merger's decision, and it goes two ways by shape.
A bare variable is replaced with the value Gazelle derived, and whatever the
variable held is gone. Two lists joined with `+` are refused: the attribute keeps
your expression and Gazelle stops recomputing it. `rules_go`'s extension behaves
the same way, since both call the same merger. Neither outcome is silent here:

```
typescript: BUILD.bazel:28: next_build(app) declares staging_srcs as an
expression Gazelle's merger cannot reconcile value by value, so staging_srcs is
no longer an attribute Gazelle maintains: it either replaces the whole
expression, losing what it computed, or leaves it untouched and stops updating
it. A "# keep" comment above the attribute makes that yours deliberately.
```

Either way the attribute has stopped being maintained. Two resolutions: put
`# keep` above the attribute and own it, or rewrite the value as a plain list of
strings with `# keep` on the entries Gazelle cannot derive, which hands the
attribute back to it.

A `glob()` is the one shape this extension decides itself, since the merger never
sees it: `srcs` on `next_build` and `sveltekit_build` is written directly. A
`glob()` whose arguments are not plain lists of strings is left alone, because
`rule.ParseGlobExpr` reads only part of such a call and a rewrite would drop the
rest. The run reports it with the file and the span of the value:

```
typescript: BUILD.bazel:28.20-28.25: could not merge expression -- next_build(app)
declares srcs that is not a glob() of plain strings, so Gazelle left it alone. It
now has to cover app/**, content/** by hand: a file srcs does not name is absent
from the staged tree and does not resolve.
```

## Examples

### Existing Codebase Without Explicit Return Types

Nothing to configure. The `ts_compile` default (`declarations = "tsgo"`) emits
declarations from the full type program, so inferred export types are fine:

```python
# BUILD.bazel (repo root)
load("@gazelle//:def.bzl", "gazelle")

gazelle(
    name = "gazelle",
    gazelle = "@rules_typescript//gazelle:gazelle_typescript",
)
```

### Opt One Package into Oxc Declaration Emit

Once every export in a package carries an explicit type, move it to Oxc's
syntactic emit to take type-checking off the critical path. See
[Isolated Declarations](../getting-started/isolated-declarations.md) for the full
explanation.

```python
# src/my-package/BUILD.bazel

# gazelle:ts_declarations oxc

# Gazelle regenerates with declarations = "oxc". Oxc fails the build, naming
# the file and line, for any export it cannot derive a type from.
```

An unrecognised value keeps the inherited emitter and logs a warning, so a typo
cannot silently demand annotations across a whole tree.

### One Target per TypeScript Project

```python
# gazelle:ts_package_boundary tsconfig
```

A directory is a package when it holds a `tsconfig.json`, and everything below
it that does not hold one of its own rolls up into it. The unit is then the same
one `tsc` compiles, which is the only way to express two shapes that are legal
in a single program and impossible to split across Bazel packages:

- An ambient declaration that types sources in another directory, and refers
  back to them. `wrangler types` writes exactly this: a `worker-configuration.d.ts`
  beside the tsconfig declaring the globals `src/` is written against, holding a
  `typeof import("./src/index")` of its own. Split by directory, the two targets
  need each other.
- Directories that import each other. At file granularity there is no cycle;
  at directory granularity there is one. That is the commonest package-level
  cycle: a barrel re-exporting `./rules` while `./rules` imports `../utils`.

A directory the covering `tsconfig.json` does not sit in becomes a package of its
own with `# gazelle:ts_package_boundary true`, which is what the diagnostic about
a framework's staged sources landing under a boundary advises.

### More than One npm Hub

A workspace can translate several lockfiles, and which hub a package's imports
come from is a property of that package, so it is named where the package is:

```python
# eslint-plugin/BUILD.bazel

# gazelle:ts_npm_hub npm_eslint
```

Generated deps in that tree then read `@npm_eslint//:eslint` rather than
`@npm//:eslint`, and a generated `ts_lint`'s `linter_binary` reads
`@npm_eslint//:eslint_bin`. Without it the label names a hub the package does
not use, and that label does not exist. Both `npm_eslint` and `@npm_eslint` are
accepted.

Declaring a hub is `npm.translate_lock(name = ...)` plus a matching `use_repo`,
and each hub needs its own `ts_add_package` target. See
[More than one hub](../guides/npm.md#more-than-one-hub).

### Point One npm Package at a Label of Your Own

The pnpm lockfile is the inventory: every package it declares resolves into the
hub. A package that has to come from somewhere else — vendored, patched, built
by a target in this repo — is named in a JSON file of npm name → Bazel label:

```json
{
  "vite": "//vendor/vite:vite"
}
```

```python
# BUILD.bazel (repo root)

# gazelle:ts_npm_mapping npm/mapping.json
```

The path is workspace-root-relative, because the labels in the file are
workspace labels. The file **overlays** the inventory rather than replacing it:
`vite` resolves to `//vendor/vite:vite` and every other package the lockfile
declares keeps its hub label, so a file listing three overrides does not shrink
the workspace's inventory to three packages. Repeat the directive, or declare
another in a subtree, to overlay again on top of what an ancestor mapped.

This is not `ts_npm_hub`, which names the *repo* a whole tree's bare specifiers
resolve into. Use the hub directive when the packages are the same and the repo
differs; use this one when a single package's label is not the hub's at all.

### Path alias for `@/` imports

```python
# BUILD.bazel (repo root)

# gazelle:ts_path_alias @/ src/
```

This maps `import { x } from "@/utils"` to `//src/utils`.

A generated target carries the aliases its own imports match, plus any alias whose
directory holds its own sources, which is exactly the set `ts_compile` accepts. It
therefore cannot trip
[its alias validation](../rules/ts-compile.md#the-two-hard-errors). The directive
reaches `compilerOptions.paths` in the
[IDE tsconfig](../getting-started/ide-setup.md) as soon as you declare it, before
any source imports through it. Aliases Gazelle reads back out of a `tsconfig.json`
this ruleset generated are not re-emitted.

### Add Runtime Deps to All Tests

```python
# BUILD.bazel (repo root)

# gazelle:ts_runtime_dep @npm//:happy-dom
# gazelle:ts_runtime_dep @npm//:react
```

These labels are appended to every generated `ts_test` deps list in the repo.

### Declare ambient `@types` once for the whole repo

```python
# BUILD.bazel (repo root)

# gazelle:ts_ambient_types @npm//:types_node
```

Appended to every generated `ts_compile` and `ts_test` deps list in the tree,
including a target whose sources import nothing at all.

This is the one dep Gazelle cannot infer. Every other dep comes from a specifier
in a source file, and an **ambient** declaration is one nothing imports: a file
using `process`, `Buffer` or `__dirname` gives the resolver nothing to work from.
A strict-deps failure over a global is therefore the one failure
`bazel run //:gazelle` cannot repair, and the alternative is adding
`@types/node` by hand to every target that touches a global.

Scope it to a subtree by putting the directive in that directory's BUILD file; it
is inherited by that tree only. Labels accumulate down the tree, so a root
`@npm//:types_node` and a `web/BUILD.bazel` `@npm//:types_react` both apply under
`web/`.

It does not widen what the compiler accepts: the dep still has to exist, and
`ts_compile` still names only direct `@types/*` deps in the tsconfig's `files`,
plus what their entries name in `/// <reference types=...>`. See
[ts_compile](../rules/ts-compile.md#types-packages).

### Suppress Generation for a Directory

```python
# legacy-code/BUILD.bazel

# gazelle:ts_ignore
```

Gazelle will not generate `ts_compile` or `ts_test` targets in `legacy-code/` or any of its subdirectories. Write the BUILD file for this directory manually.

### Register a Codegen Target

```python
# src/api/BUILD.bazel

# gazelle:ts_codegen api_types @npm//:openapi-typescript_bin api-types.ts {srcs} -o {out}
```

The fields are `<name> <generator_label> <outs> [srcs:<csv>] [args…]`, where
`<outs>` is a comma-separated list of output file names and everything after the
optional `srcs:` field is passed to the generator. `{srcs}` and `{out}` are
substituted. Without a `srcs:` field the generator reads the directory's own
TypeScript sources, which is what a route-tree or barrel generator wants; a
generator that reads a schema names it:

```python
# gazelle:ts_codegen schema_types //tools:schemagen schema.gen.ts srcs:schema.graphql --out {out}
```

A `srcs:` entry may be a `glob()` call instead of a file name, and the two mix —
a generator reading one settings file plus a directory of catalogues names both:

```python
# gazelle:ts_codegen messages //tools:paraglide dir:compiled srcs:settings.json,glob(["messages/*.json"]) --outdir {out}
```

which writes `srcs = ["settings.json"] + glob(["messages/*.json"])`. Commas
inside the call belong to it, so a glob may carry several patterns and an
`exclude`. The field takes no whitespace: a directive is split on spaces before
anything else, so write `glob(["a/*.json","b/*.json"])`, not `glob(["a/*.json", "b/*.json"])`.

The directive is inherited by subdirectories the way every directive is, but the
target it names is written in the one directory the directive was written in.

Alongside the `ts_codegen`, Gazelle writes `<name>_compile`: the `ts_compile`
that takes the codegen label in `srcs`, with `declarations = "oxc"`. That is the
target an import of a generated module resolves to — `ts_compile.deps` takes
providers `ts_codegen` does not return, so the generated source reaches a compile
through `srcs`, and under the default `declarations = "tsgo"` emit one target
cannot hold both checked-in and generated sources. See
[Compiling the output](../rules/ts-codegen.md#compiling-the-output).

A declared out that is also checked in is kept out of the package's
`ts_compile.srcs`: a file that is both a source and an output of its package is
a conflicting declaration Bazel rejects. Deleting the checked-in copy is
optional, and nothing changes on the day you do.

For a generator that writes a whole directory, prefix the outs field with
`dir:`:

```python
# gazelle:ts_codegen prisma_client @npm//:prisma_bin dir:generated/client generate --schema {srcs}
```

The `dir:` form gets no `<name>_compile` and nothing in it resolves: Bazel
declares the directory as one artifact, so no file inside it has a label, and
`ts_compile` takes neither a directory in `srcs` nor a `ts_codegen` in `deps`.
Reaching the output means writing a rule that adapts the directory to the
providers `ts_compile.deps` reads, and depending on that by hand. The directive
writes the target; wiring it up is yours.

Gazelle also auto-detects Prisma, GraphQL codegen and OpenAPI generators, so a
directive is only needed for a generator it does not recognise. Each of those
three needs both halves in the same directory: the input file (`schema.prisma`,
a `.graphql`/`.gql` file, `openapi.yaml` and its variants) and the generator's
own npm dependency. One without the other emits nothing, which is what keeps a
monorepo's shared `package.json` from generating targets everywhere.

TanStack Router is deliberately excluded: its route tree is written by the Start
Vite plugin during the bundle, into the writable staging directory `ts_bundle`
hands it, so a second copy in `bazel-bin` would only drift from the one the build
used.

### Declare what an asset extension imports as

`asset_library` writes a `<asset>.<ext>.d.ts` beside every asset it covers, and
by default it says `string` — the URL a bundler hands back when it does not
transform the file. A project running svgr gets a component from `*.svg`
instead, and a `declare module "*.svg"` of its own does not fix it: TypeScript
prefers the concrete declaration beside the asset over any pattern. The
attribute that says so is `asset_library.declaration_type`, and this is the
directive that fills it in:

```python
# BUILD.bazel

# gazelle:ts_asset_declaration_type .svg import("react").FC<import("react").SVGProps<SVGSVGElement>>
```

Every `asset_library` in this directory and below whose `srcs` hold a `.svg`
carries that type from the next run on — the ones Gazelle writes and the ones it
has already written, which is the whole point: Gazelle writes one target per
asset file, so a repo of any size has too many of them to edit by hand.

Only the first space separates the extension from the expression, unlike
`ts_codegen` above, so an expression with spaces in it needs no quoting:

```python
# gazelle:ts_asset_declaration_type .md { default: string; toc: string[] }
```

One directive per extension; a target takes an entry only for the extensions its
own `srcs` have. A subdirectory overrides what it inherited by naming the
extension again, and returns the subtree to the `string` default by naming the
extension with nothing after it:

```python
# packages/legacy/BUILD.bazel

# These are imported as URLs; the svgr transform does not run here.
# gazelle:ts_asset_declaration_type .svg
```

The bare form is not "stop managing it": Gazelle removes the entry from the
targets in that subtree, including one an inherited directive wrote on an
earlier run. To hold a value against the directive, `# keep` it — on the entry,
the attribute, or the rule:

```python
asset_library(
    name = "sprite_svg",
    srcs = ["sprite.svg"],
    declaration_type = {
        # keep
        ".svg": "string",
    },
)
```

An extension no directive in scope names is not Gazelle's at all, so a repo that
hand-wrote `declaration_type` before adopting the directive keeps every entry it
wrote until a directive names that extension.

The expression is written into the generated `.d.ts` verbatim and nothing checks
it: a name that does not resolve widens the import to `any` in silence, because
the declaration is a `.d.ts` and this ruleset compiles with `skipLibCheck`.
Building with `--//ts:lib_check`, or the consuming target alone with
`compiler_options = {"skipLibCheck": False}`, is what surfaces it; the error
names the generated `<asset>.d.ts`, whose header names the target and the
attribute. See
[`asset_library`](../rules/css-and-assets.md#when-an-asset-is-not-a-url).

### Admit a `.mjs` or `.cjs` into `srcs`

`ts_compile` has always accepted `.js`, `.mjs` and `.cjs` in `srcs`: they are
staged into the output tree unchanged and added to the type program, and under
the default `declarations = "tsgo"` each one gets a declaration
(`.d.ts` / `.d.mts` / `.d.cts`) the way `tsc` emits one. Gazelle does not put
them there on its own, because most `.mjs` in a repository is configuration —
`eslint.config.mjs`, `postcss.config.mjs` — and type-checking those is nobody's
intent. This directive is the opt-in:

```python
# scripts/BUILD.bazel

# gazelle:ts_js_srcs .mjs .cjs
```

A `scripts/lib/helper.mjs` beside the `helper.test.ts` that imports
`./helper.mjs` is now a src of the target that compiles the test, and the import
resolves. Without it that `.mjs` belongs to no generated target at all, and the
import fails the type check as an unresolved module (`TS2307`), which is the one
symptom this directive is for.

The value is the whole set, so a subdirectory naming one extension admits that
one alone, and naming none returns the subtree to `.ts`/`.tsx`:

```python
# scripts/vendor/BUILD.bazel

# Vendored, and not ours to type-check.
# gazelle:ts_js_srcs
```

Plain `.js` is not admissible, and the directive refuses it by name: `ts_compile`
already declares `<stem>.js` as the output of a `.ts` src of the same stem, so a
checked-in `foo.js` beside `foo.ts` would be one file declared twice and fail
analysis. `.mjs` and `.cjs` get `.d.mts` / `.d.cts` instead and cannot collide.

Admission is about `srcs` and nothing else. What makes a directory a package in
`tsconfig` mode is still a `tsconfig.json`, and a framework entry point is still
`.ts`/`.tsx`: an admitted `.mjs` is compiled by the target that claims it, and is
not a reason for a directory to become one or for an app to boot from it.
`checkJs` is off, as it is in `ts_compile` — the JSDoc types in an admitted
file cross the package boundary, and the file's own body is not checked unless
`compiler_options` says so.

### A glob does not cross a package boundary

`glob()` is evaluated in the package holding the rule and does not descend into
a subpackage, and Bazel refuses to load a package whose glob matched nothing
(`allow_empty` is `False`). So a pattern reaching into a subdirectory only works
while that subdirectory has no BUILD file of its own — and a directory of
message catalogues is exactly the kind of directory Gazelle would otherwise put
one in, a `json_library` per file.

Gazelle therefore leaves such a directory alone: the files an ancestor's
`ts_codegen` glob collects are that rule's inputs, so they get no targets of
their own and the directory stays part of the package above it. This holds only
while the glob collects *everything* Gazelle would write a target for there. One
file it does not — a stray `.ts`, a `README.md` — still needs a target, that
target makes the directory a package again, and the ancestor's glob goes empty.
Gazelle logs which files those are; the fixes are to move them out, or to put
the tree under `# gazelle:ts_package_boundary tsconfig`, where a subdirectory
holding no `tsconfig.json` of its own is not a package to begin with.

### Exclude Generated Files

```python
# src/graphql/BUILD.bazel

# gazelle:ts_exclude *.generated.ts
```

Files matching `*.generated.ts` are excluded from `srcs` lists in this directory
and every directory below it. Nothing is excluded by name on its own: a
checked-in file is a source unless a rule in the package declares it as an
output, however it is named.

A pattern with no `/` in it is matched against the **basename**, so it drops a
file of that name at every depth below the declaration. That is what
`*.generated.ts` is for, and it is usually not what naming a single file means: a
bare `vite.config.ts` in `web/BUILD.bazel` also drops any future
`web/**/vite.config.ts`.

#### Anchoring a pattern to one path

A leading `./` resolves the rest of the pattern against the directory whose build
file declares it, and matches it against the path rather than the name:

```python
# web/BUILD.bazel

# gazelle:ts_exclude ./vite.config.ts
```

drops `web/vite.config.ts` and nothing else — `web/sub/vite.config.ts` keeps its
target. The path can be any depth, so `# gazelle:ts_exclude ./plugins/one.ts` in
`web/BUILD.bazel` names `web/plugins/one.ts`, and one directive reaches a file
below the directory it is written in. A `*` does not cross a `/`, as it does not
in `filepath.Match` or in a Bazel `glob`, so `./*.gen.ts` covers the declaring
directory's own files and no subdirectory's.

Bare patterns are unchanged. `*.generated.ts` still matches a name at any depth,
and a bare pattern that does carry a `/` — `sub/*.ts` — still matches the path a
rolled-up file was reached by, relative to the package that claims it.

`./` on its own has no path after it, so it resolves to the declaring
directory's own path — and nothing a package reaches is ever compared against
that, so the pattern excludes nothing at all. The run says so rather than
accepting a directive that cannot do anything.

#### Naming a directory depends on the boundary mode

A directory name is read in two places, not one: the rollup walk, and the
framework bundle's staging walk. Both reach only a subdirectory that is **not**
a package -- the rollup walk runs in `tsconfig` mode alone, and the staging walk
covers exactly the directories the framework does not own, since an owned one is
staged by the label its own package exports. So under the default `every-dir`
mode, where a subdirectory holding sources is a package in its own right, a
directory pattern still reaches nothing through either.

| In `web/BUILD.bazel`, with `web/sub/s.ts` | default `every-dir` | `tsconfig` |
| --- | --- | --- |
| `# gazelle:ts_exclude sub` (or `./sub`) | `web/sub` is still its own package and still compiles `s.ts`, and a framework bundle still stages it | `web` does not roll `sub/s.ts` up, so no target compiles it |
| `# gazelle:exclude sub` (Gazelle's own) | the walk is pruned: no BUILD file in `web/sub`, and nothing compiles `s.ts` | the walk is pruned, but the rollup walk is not: `web` still claims `sub/s.ts` |

So under the default mode, dropping a whole directory is `# gazelle:exclude`
(Gazelle's own directive, which prunes the walk) or a `# gazelle:ts_ignore` in
that directory's own BUILD file — not `ts_exclude`.

#### What a pattern drops is reported

A run says what each pattern took out of the targets it generated, one line per
pattern per package:

```
typescript: web: # gazelle:ts_exclude vite.config.ts leaves 1 TypeScript file
out of the srcs generated here: web/vite.config.ts. It names no path, so it
matches that basename at every depth below this directory; "./vite.config.ts"
anchors it here.
```

The count is the tell: that is how a pattern matching more than it meant to gets
caught, rather than by noticing months later that a file is not in the build. The
line is bounded — three names and a count for the rest — and a pattern that
matched nothing in a package says nothing there, so a root-level
`*.generated.ts` is quiet everywhere it does not apply.

One line per package is what a bare pattern gets, because it reaches every
package below the declaration: under the default `every-dir` mode a namesake in
`web/sub` is a package of its own and is reported in its own line there. Under a
rolled-up boundary `web` claims the subtree, and one line carries the whole
count:

```
typescript: web: # gazelle:ts_exclude vite.config.ts leaves 2 TypeScript files
out of the srcs generated here: web/sub/vite.config.ts, web/vite.config.ts. It
names no path, so it matches that basename at every depth below this directory;
"./vite.config.ts" anchors it here.
```

Directives are inherited, so the package a drop fires in is usually not the
package holding the line to edit. The line names the declaring build file when
those differ, and spells the anchored form so that writing it *there* names the
package the drop fired in:

```
typescript: web: # gazelle:ts_exclude *.gen.ts, declared in the workspace root,
leaves 1 TypeScript file out of the srcs generated here: web/mod.gen.ts. It names
no path, so it matches that basename at every depth below the workspace root;
"./web/*.gen.ts" in that build file matches web's own files only.
```

The claim is about the srcs of that run and no further. Exclusion happens at
generation time and never sees the merge, and `rule.MergeList` keeps a list
element carrying `# keep` — so a hand-kept `srcs` entry goes on compiling an
excluded file:

```python
ts_compile(
    name = "web",
    srcs = [
        "app.ts",
        "vite.config.ts",  # keep — survives # gazelle:ts_exclude vite.config.ts
    ],
)
```

A pattern that names a **directory** is not reported. Where it does anything at
all it stops the rollup walk before reading what is inside, and Gazelle does not
walk a subtree only to count what the exclusion exists to skip.

#### `ts_exclude_dir`, when Gazelle should not enter at all

`ts_exclude` drops files from the targets of packages Gazelle still walks.
`ts_exclude_dir` keeps it out of a directory entirely, by basename:

```python
# BUILD.bazel (repo root)

# gazelle:ts_exclude_dir coverage
# gazelle:ts_exclude_dir storybook-static
```

Nothing is generated in any `coverage/` or `storybook-static/` below the root,
on top of the built-in `.next`, `.nuxt`, `.svelte-kit`, `dist` and `build`. The
directive goes in an *ancestor* because a directory Gazelle should not enter is
exactly the kind with no build file of its own, and writing one there to say
"ignore me" is backwards — that is what `ts_ignore` is for, in a directory whose
BUILD file you are keeping anyway.

The value is a basename, and the whole value is one name. A path, a glob or a
list of names is refused out loud, because the traversal only ever compares one
directory basename against it. `ts_exclude` is not the way to reach a directory
either: its patterns drop files from a target's `srcs`, and under the default
`every-dir` boundary the directory is its own target, so an anchored path never
gets there.

Repeat the directive for each name. A nested build file's directives **append**
to the set it inherits, so the effective set does not depend on which directory
asks:

```python
# apps/BUILD.bazel

# gazelle:ts_exclude_dir fixtures
```

Under `apps/` Gazelle skips `coverage`, `storybook-static` and `fixtures`.
