# Gazelle Directives Reference

Directives go in `BUILD.bazel` files as comments and control how Gazelle generates BUILD rules for that directory and its children.

## Full Reference

| Directive | Effect |
|-----------|--------|
| `# gazelle:ts_declarations oxc` | Emit `declarations = "oxc"` on generated `ts_compile` and `ts_test` rules in this tree: syntactic `.d.ts` emit, so every export needs an explicit type |
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
instead: the modes decide which files each target compiles.

!!! note "Upgrading"
    `# gazelle:ts_package_boundary index-only`, which made a directory a package
    only when it held an `index.ts` or `index.tsx`, is gone, and the run stops
    on it: `ts_package_boundary index-only was removed; the modes are
    "every-dir" and "tsconfig"`. Move the tree to
    `# gazelle:ts_package_boundary tsconfig`, add a `tsconfig.json` to each
    directory that is to be a package, and `# gazelle:ts_package_boundary true`
    to any that has to be one without holding the file. See
    [One target per TypeScript project](#one-target-per-typescript-project).

    `gazelle_ts.json` is gone too; nothing reads it. Each key becomes a
    directive, and a file left behind lands in a generated `json_library`. The
    key-to-directive table is under
    [Configuration](overview.md#configuration).

## `# keep`

`# keep` is Gazelle's own comment, understood by every language extension. Above
an attribute it means "never rewrite this value"; above a whole rule, "never
rewrite this rule". Use it when a run keeps undoing an edit:

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
visibility widens back on every run that re-emits the rule. See
[getting the clean-tree diff to empty](overview.md#getting-the-clean-tree-diff-to-empty).

### Attributes Gazelle Owns

Gazelle recomputes these from the tree on every run, so a value it cannot derive
is replaced unless a `# keep` holds it. `ts_compile.deps` and
`ts_config.deps` are equally Gazelle's:

| Rule | Attributes Gazelle owns |
|------|-------------------------|
| `ts_compile` | `srcs`, `deps`, `visibility`, `path_aliases`, `path_alias_srcs`, `declarations`, `tsconfig` |
| `ts_test` | `srcs`, `deps`, `tsconfig`, `path_aliases`, `path_alias_srcs` |
| `ts_config` | `src`, `deps`, `visibility` |
| `ts_lint` | `srcs`, `linter`, `linter_binary`, `config`, `fail_on_warnings` |
| `asset_library` | `declaration_type`, one entry per extension a `ts_asset_declaration_type` directive names; an extension no directive names is yours |
| `ts_codegen` | `outs`, `out_dir`, `visibility` |
| `filegroup(name = "tsconfig_types")` | `srcs`, `visibility` |

`ts_config.deps` is the `extends` chain. Gazelle writes it from the one
specifier shape it can read without guessing: a single relative path naming an
ancestor directory's own `tsconfig.json`. Every other shape gets no value, and
for an owned attribute that means the value goes: a hand-written `deps` needs a
`# keep` on its line to survive the next run. See
[the compilerOptions baseline](overview.md#the-compileroptions-baseline).

!!! note "Upgrading"
    `ts_config.deps`, `ts_test.path_aliases`, and `path_alias_srcs` on
    `ts_compile` and `ts_test` used to be write-once; they are Gazelle's now.
    On every `ts_config` whose `deps` you wrote by hand, every `ts_test` whose
    `path_aliases` you did, and every `ts_compile` or `ts_test` whose
    `path_alias_srcs` you did, put `# keep` on the entry's own line to hold that
    entry beside what Gazelle computes, or above the attribute to keep the whole
    value. Without one, `deps` and `path_aliases` are recomputed entry by entry
    and each dropped entry is named in the log; `path_alias_srcs` is filled in
    after resolution, and a label dropped there is not reported.

`path_aliases` holds the aliases a target's own imports go through, on the
`ts_compile` and the `ts_test` alike: the test files are a program of their own,
and the package target's map reaches nothing they compile. `path_alias_srcs` is
filled in at resolve time the way `deps` is, and only when no src of the target
sits under the alias directory: it names the target the aliased import resolved
to, whose outputs are then the staged files `ts_compile`'s alias guard finds
under the directory. A target with a src under the directory validates the alias
on that src and gets no `path_alias_srcs`, since the aliased declarations already
arrive on the dep edge. See
[which targets carry an alias](overview.md#which-targets-carry-an-alias).

`ts_compile.public_globals` is absent. Whether a `.d.ts`'s globals are part of
the package's public type surface is a decision nothing in the source states, so
no directive writes it and a hand-written value survives every run, `# keep` or
not.

`types` and `types_srcs` are a third case: generated, and not owned. Gazelle
writes `types` where the nearest `tsconfig.json` names entries in
`compilerOptions.types` and a label stages every file among them, and
`types_srcs` beside it where there is such a file; see
[a declaration the tsconfig names](overview.md#a-declaration-the-tsconfig-names).
Neither is mergeable: the value on disk wins, except the one label below that
Gazelle takes back, and `rule.MergeRules` copies in an attribute the rule does
not carry at all. **Deleting the lines does not opt out**: they come back on
the next run. The label Gazelle takes back is a `types_srcs` entry naming the
`tsconfig_types` filegroup of the rule's own package or of a package above it,
the only ones Gazelle writes into a rule, spelled `:tsconfig_types` or
`//pkg:tsconfig_types` as Gazelle spells them, on a `ts_compile` or `ts_test`
on disk whose kind and name match a rule the run generates in that package; a
rule by any other name is not read. Where the run does not write that
filegroup, because the file moved into a `ts_codegen`'s `outs` or is gone, the
entry is replaced by the labels the run stages the rule's own entries by, or
dropped when there are none, and the run says so per rule. An entry naming a
filegroup the run writes stays, on a rule whose own tsconfig names no file too,
and so does one naming a filegroup a `# keep` holds under that name in its
package, which the run leaves in place; so does one naming anything else, the
`tsconfig_types` of a package elsewhere in the tree included. `# keep` on the
entry or above the attribute holds even one Gazelle wrote. Two things stick.
`types = []` with `types_srcs = []` keeps both attributes present and asks for
no ambient types at all, dropping the package entries the tsconfig named along
with the file. A `# keep` above the whole `ts_compile` keeps whatever you wrote
and leaves the entries where `extends` puts them, unresolved.

Six kinds are the exception: `ts_pnpm`, `ts_add_package`, `css_library`,
`css_module`, `asset_library` and `json_library`. Each is written once and left
alone while it holds its claim: Gazelle emits no candidate for it, so the merger
never runs on it, and its attributes are yours from the second run on, `# keep`
or not; `asset_library.declaration_type` is written into the existing rule, as
the table says. The first two claim a name and are written when no rule of that
name exists. A data-file rule claims a file. Gazelle writes one when the file is
in no plain `srcs` of a `ts_compile`, `ts_test`, `css_library`, `css_module`,
`asset_library` or `json_library` in the package; the `srcs` it recomputes on
the `ts_compile` and `ts_test` targets it writes itself do not count, and
neither does a `filegroup` naming the file. Later runs read the rule's own
`srcs` as the claim, so a `glob()` there claims nothing: the rule gets a
candidate on every run, `visibility` widens back, and the merger logs the
`glob()` it cannot merge. When the file is deleted, the next run removes the
rule, as it removes a `ts_compile` whose plain `srcs` name only files that are
gone, with the `ts_lint` beside it; `# keep` above the rule holds it, and above
the `ts_compile` holds the `ts_lint` too. A `srcs` holding a label or a
`glob()` is not judged for either kind. A file a rule in the package generates
counts as present for both, except a declaration a `ts_codegen` in the package
writes, which counts as gone for the package `ts_compile`: that target's label
stages it, and no plain `srcs` lists it. `ts_add_package` declares `pnpm_lock`
mergeable and no merge ever reaches it.

`ts_dev_server` is outside all of this: Gazelle does not write or touch it;
write it by hand. Gazelle knows no such kind, so the rule and its load symbol
come through every run as written, `# keep` or not. See
[Dev Server](../guides/dev-server.md).

`# keep` works at three granularities: one value, one attribute, one rule. Every
write path honours all three: the merger's, the entry-by-entry merge
`path_aliases` needs, since the merger has no case for a dict, and the
`types_srcs` rewrite above:

```python
ts_compile(
    name = "app",
    srcs = [
        "main.ts",
        "legacy.js",  # keep
    ],
    # One alias entry no import implies: a directory a codegen action writes.
    path_aliases = {
        "@/": "src/",
        "@gen/": "src/generated/",  # keep
    },
    # keep
    tsconfig = "//:tsconfig_build",
)
```

A run that drops a value from one of these attributes reports it:

```
typescript: ts_compile(app) in BUILD.bazel: Gazelle generates srcs and
recomputed it from the tree, so "legacy.js" is no longer declared. A
value Gazelle cannot derive needs a "# keep" comment on its own line to survive
the next run; "# keep" above the attribute hands the whole attribute back to you.
```

Every dropped value is reported, whether it was a stale label Gazelle wrote
itself or an edit of yours. `deps` and `path_alias_srcs` are the exception: they
are filled in after resolution, when the value on disk is no longer in hand, so
a label you wrote into either goes without a report unless `# keep` holds it.
Gazelle's Go extension drops the same values silently; what survives a run is
identical either way.

One case is silent: a value whose file is no longer on disk. Deleting a source
drops the entry that named it, and holding that entry with `# keep` would name a
source nothing provides, which fails analysis. A value whose file is still on
disk is always reported.

#### Values Gazelle Cannot Merge

`# keep` decides what survives among plain strings and plain lists of plain
strings, the two shapes Gazelle's merger reconciles value by value. A value in
any other shape it cannot merge at all: a module-level variable, two lists joined
with `+`, a `select()`, or a list with one variable element.

What happens then is the merger's decision, and it goes two ways by shape. A
bare variable is replaced with the value Gazelle derived, and whatever the
variable held is gone. Two lists joined with `+` are refused: the attribute keeps
your expression and Gazelle stops recomputing it. `rules_go`'s extension behaves
the same way, since both call the same merger. Neither outcome is silent here:

```
typescript: BUILD.bazel:28: ts_compile(app) declares srcs as an
expression Gazelle's merger cannot reconcile value by value, so srcs is
no longer an attribute Gazelle maintains: it either replaces the whole
expression, losing what it computed, or leaves it untouched and stops updating
it. A "# keep" comment above the attribute makes that yours deliberately.
```

Either way the attribute has stopped being maintained. Two resolutions: put
`# keep` above the attribute and own it, or rewrite the value as a plain list of
strings with `# keep` on the entries Gazelle cannot derive, which hands the
attribute back to it.

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
[Isolated Declarations](../getting-started/isolated-declarations.md).

```python
# src/my-package/BUILD.bazel

# gazelle:ts_declarations oxc

# Gazelle regenerates with declarations = "oxc". Oxc fails the build, naming
# the file and line, for any export it cannot derive a type from.
```

An unrecognised value keeps the inherited emitter and logs a warning.

### One Target per TypeScript Project

```python
# gazelle:ts_package_boundary tsconfig
```

A directory is a package when it holds a `tsconfig.json`, and everything below
it that does not hold one of its own rolls up into it. The unit is then the same
one `tsc` compiles. Two shapes are legal in a single program and impossible to
split across Bazel packages:

- An ambient declaration that types sources in another directory, and refers
  back to them. `wrangler types` writes exactly this: a `worker-configuration.d.ts`
  beside the tsconfig declaring the globals `src/` is written against, holding a
  `typeof import("./src/index")` of its own. Split by directory, the two targets
  need each other.
- Directories that import each other. At file granularity there is no cycle;
  at directory granularity there is one. The commonest package-level cycle is a
  barrel re-exporting `./rules` while `./rules` imports `../utils`.

A directory the covering `tsconfig.json` does not sit in becomes a package of its
own with `# gazelle:ts_package_boundary true`.

### More than One npm Hub

A workspace can translate several lockfiles. Which hub a package's imports come
from is a property of that package, so it is named where the package is:

```python
# eslint-plugin/BUILD.bazel

# gazelle:ts_npm_hub npm_eslint
```

Generated deps in that tree then read `@npm_eslint//:eslint`, not
`@npm//:eslint`, and a generated `ts_lint`'s `linter_binary` reads
`@npm_eslint//:eslint_bin`. Without it the label names a hub the package does
not use, and that label does not exist. Both `npm_eslint` and `@npm_eslint` are
accepted.

Two generated labels do not follow the directive yet and still name `@npm`:
the `ts_codegen` generator Gazelle detects (`@npm//:prisma_bin`), and the deps
a tsconfig `types` entry produces.

Declaring a hub is `npm.translate_lock(name = ...)` plus a matching `use_repo`,
and each hub needs its own `ts_add_package` target. See
[More than one hub](../guides/npm.md#more-than-one-hub).

### Point One npm Package at a Label of Your Own

The pnpm lockfile is the inventory: every package it declares resolves into the
hub. A package that has to come from somewhere else (vendored, patched, built
by a target in this repo) is named in a JSON file of npm name → Bazel label:

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
workspace labels. The file **overlays** the inventory: `vite` resolves to
`//vendor/vite:vite` and every other package the lockfile declares keeps its hub
label, so a file listing three overrides does not shrink the workspace's
inventory to three packages. Repeat the directive, or declare another in a
subtree, to overlay again on top of what an ancestor mapped.

`ts_npm_hub` names the repo a whole tree's bare specifiers resolve into; this
directive names one package's label. Use the hub directive when the packages are
the same and the repo differs, and this one when a single package's label is not
the hub's at all.

### Path Alias for `@/` Imports

```python
# BUILD.bazel (repo root)

# gazelle:ts_path_alias @/ src/
```

This maps `import { x } from "@/utils"` to `//src/utils`.

A generated target carries the aliases its own imports match, plus any alias whose
directory holds its own sources, which is exactly the set `ts_compile` accepts. It
cannot trip
[its alias validation](../rules/ts-compile.md#the-two-hard-errors). The directive
reaches `compilerOptions.paths` in the
[IDE tsconfig](../getting-started/ide-setup.md) as soon as you declare it, before
any source imports through it. An alias read back out of a `tsconfig.json` this
ruleset generated gets no such entry: it is written only on a target whose own
imports go through it.

### Add Runtime Deps to All Tests

```python
# BUILD.bazel (repo root)

# gazelle:ts_runtime_dep @npm//:happy-dom
# gazelle:ts_runtime_dep @npm//:react
```

These labels are appended to every generated `ts_test` deps list in the repo.

### Declare Ambient `@types` Once for the Whole Repo

```python
# BUILD.bazel (repo root)

# gazelle:ts_ambient_types @npm//:types_node
```

Appended to every generated `ts_compile` and `ts_test` deps list in the tree,
including a target whose sources import nothing at all.

This is the one dep Gazelle cannot infer. Every other dep comes from a specifier
in a source file, and an **ambient** declaration is one nothing imports: a file
using `process`, `Buffer` or `__dirname` gives the resolver nothing to work from.
A strict-deps failure over a global is the one failure `bazel run //:gazelle`
cannot repair; the alternative is adding `@types/node` by hand to every target
that touches a global.

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
TypeScript sources, which is what a route-tree or barrel generator wants. A
generator that reads a schema names it:

```python
# gazelle:ts_codegen schema_types //tools:schemagen schema.gen.ts srcs:schema.graphql --out {out}
```

A `srcs:` entry may be a `glob()` call instead of a file name, and the two mix.
A generator reading one settings file plus a directory of catalogues names both:

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
target an import of a generated module resolves to. `ts_compile.deps` takes
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

The `dir:` form gets no `<name>_compile`: Bazel declares the directory as one
artifact, so no file inside it has a label to put in a `ts_compile`'s `srcs`.
The target itself returns the providers `ts_compile.deps` reads, and Gazelle
resolves an import of a module under the directory to the `ts_codegen` label,
by the `out_dir` path a relative or aliased specifier reaches or by the
target's `module_name`. Nothing compiles the tree, so the generator has to write
`.js` beside `.d.ts`. See
[a directory of output](../rules/ts-codegen.md#a-directory-of-output).

The directory is the target's output whether or not a local run of the
generator has left a copy on disk: Gazelle lists nothing under it in any
`srcs`, and writes no BUILD file at or below it in either boundary mode. A
detected generator and a `ts_codegen` written by hand count the same as one a
directive wrote.

Gazelle also auto-detects Prisma, GraphQL codegen and OpenAPI generators, so a
directive is only needed for a generator it does not recognise. Each of those
three needs both halves in the same directory: the input file (`schema.prisma`;
a `.graphql`/`.gql` file beside a `codegen.ts`, `codegen.yml`, `codegen.yaml` or
`codegen.json`; `openapi.yaml`, `openapi.yml`, `openapi.json` or the `swagger.*`
spelling of each) and the generator's own npm dependency (`prisma` or
`@prisma/client`, `@graphql-codegen/cli`, `openapi-typescript`). Where the
workspace has a `pnpm-lock.yaml`, one without the other emits nothing, which
keeps a monorepo's shared `package.json` from generating targets everywhere.

TanStack Router is excluded: its route tree is written by the Start Vite plugin
during the build, so a second copy in `bazel-bin` would drift from the one the
build used.

### Declare What an Asset Extension Imports As

`asset_library` writes a `<asset>.<ext>.d.ts` beside every asset it covers, and
by default it says `string`: the URL a bundler hands back when it does not
transform the file. A project running svgr gets a component from `*.svg`
instead, and a `declare module "*.svg"` of its own does not fix it: TypeScript
prefers the concrete declaration beside the asset over any pattern. The
attribute that says so is `asset_library.declaration_type`, and this directive
fills it in:

```python
# BUILD.bazel

# gazelle:ts_asset_declaration_type .svg import("react").FC<import("react").SVGProps<SVGSVGElement>>
```

Every `asset_library` in this directory and below whose `srcs` hold a `.svg`
carries that type from the next run on, the ones Gazelle writes and the ones it
has already written. Gazelle writes one target per asset file, so a repo of any
size has too many of them to edit by hand.

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
earlier run. To hold a value against the directive, `# keep` it, on the entry,
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
`compiler_options = {"skipLibCheck": False}`, surfaces it; the error names the
generated `<asset>.d.ts`, whose header names the target and the attribute. See
[`asset_library`](../rules/css-and-assets.md#when-an-asset-is-not-a-url).

### Admit a `.mjs` or `.cjs` into `srcs`

`ts_compile` accepts `.js`, `.mjs` and `.cjs` in `srcs`: they are staged into
the output tree unchanged and added to the type program, and under the default
`declarations = "tsgo"` each one gets a declaration (`.d.ts` / `.d.mts` /
`.d.cts`) the way `tsc` emits one. Gazelle does not put them there on its own:
most `.mjs` in a repository is configuration (`eslint.config.mjs`,
`postcss.config.mjs`). This directive is the opt-in:

```python
# scripts/BUILD.bazel

# gazelle:ts_js_srcs .mjs .cjs
```

A `scripts/lib/helper.mjs` beside the `helper.test.ts` that imports
`./helper.mjs` is now a src of the target that compiles the test, and the import
resolves. Without it that `.mjs` belongs to no generated target at all, and the
import fails the type check as an unresolved module (`TS2307`).

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

A `.d.mts` or `.d.cts` needs no directive. It is a declaration, and Gazelle
classifies it as it classifies a `.d.ts`: a script-mode one is ambient and joins
every target in the directory, a module one joins the package target, and the
target holding `compile.d.mts` answers for `./compile.mjs`. A test importing an
untyped `.mjs` beside its hand-written declaration gets the dep edge with the
JavaScript admitted or not. Nothing checks in an `eslint.config.d.mts`.

Admission is about `srcs` and nothing else. What makes a directory a package in
`tsconfig` mode is still a `tsconfig.json`: an admitted `.mjs` is compiled by the
target that claims it, and is not a reason for a directory to become one.
`checkJs` is off, as it is in `ts_compile`: the JSDoc types in an admitted file
cross the package boundary, and the file's own body is not checked unless
`compiler_options` says so.

### Globs Across Package Boundaries

`glob()` is evaluated in the package holding the rule and does not descend into
a subpackage, and Bazel refuses to load a package whose glob matched nothing
(`allow_empty` is `False`). So a pattern reaching into a subdirectory only works
while that subdirectory has no BUILD file of its own. A directory of message
catalogues is the kind of directory Gazelle would otherwise put one in, a
`json_library` per file.

Gazelle leaves such a directory alone: the files an ancestor's `ts_codegen` glob
collects are that rule's inputs, so they get no targets of their own and the
directory stays part of the package above it. This holds only while the glob
collects everything Gazelle would write a target for there. One file it does not
(a stray `.ts`, a `README.md`) still needs a target, that target makes the
directory a package again, and the ancestor's glob goes empty. Gazelle logs which
files those are. The fixes are to move them out, or to put the tree under
`# gazelle:ts_package_boundary tsconfig`, where a subdirectory holding no
`tsconfig.json` of its own is not a package to begin with.

### Exclude Generated Files

```python
# src/graphql/BUILD.bazel

# gazelle:ts_exclude *.generated.ts
```

Files matching `*.generated.ts` are excluded from `srcs` lists in this directory
and every directory below it. One name is excluded on its own: `routeTree.gen.ts`
(or `.tsx`), which the TanStack Start Vite plugin writes during the build. Every
other checked-in file is a source unless a rule in the package declares it as an
output, however it is named.

A pattern with no `/` in it is matched against the **basename**, so it drops a
file of that name at every depth below the declaration. That is what
`*.generated.ts` is for, and it is usually not what naming a single file means: a
bare `vite.config.ts` in `web/BUILD.bazel` also drops any future
`web/**/vite.config.ts`.

#### Anchoring a Pattern to One Path

A leading `./` resolves the rest of the pattern against the directory whose build
file declares it, and matches it against the path, not the name:

```python
# web/BUILD.bazel

# gazelle:ts_exclude ./vite.config.ts
```

drops `web/vite.config.ts` and nothing else; `web/sub/vite.config.ts` keeps its
target. The path can be any depth, so `# gazelle:ts_exclude ./plugins/one.ts` in
`web/BUILD.bazel` names `web/plugins/one.ts`, and one directive reaches a file
below the directory it is written in. A `*` does not cross a `/`, as it does not
in `filepath.Match` or in a Bazel `glob`, so `./*.gen.ts` covers the declaring
directory's own files and no subdirectory's.

Bare patterns are unchanged. `*.generated.ts` still matches a name at any depth,
and a bare pattern that does carry a `/` (`sub/*.ts`) still matches the path a
rolled-up file was reached by, relative to the package that claims it.

`./` on its own has no path after it, so it resolves to the declaring
directory's own path, and nothing a package reaches is ever compared against
that: the pattern excludes nothing at all. The run says so and drops the
directive.

#### Directory Patterns and the Boundary Mode

A directory name is read in one place: the rollup walk, which runs in
`tsconfig` mode alone. Under the default `every-dir` mode a subdirectory holding
sources is a package in its own right, and a directory pattern changes nothing
about what it compiles.

| In `web/BUILD.bazel`, with `web/sub/s.ts` | default `every-dir` | `tsconfig` |
| --- | --- | --- |
| `# gazelle:ts_exclude sub` (or `./sub`) | `web/sub` is still its own package and still compiles `s.ts` | `web` does not roll `sub/s.ts` up, so no target compiles it |
| `# gazelle:exclude sub` (Gazelle's own) | the walk is pruned: no BUILD file in `web/sub`, and nothing compiles `s.ts` | the walk is pruned, but the rollup walk is not: `web` still claims `sub/s.ts` |

Under the default mode, dropping a whole directory is `# gazelle:exclude`
(Gazelle's own directive, which prunes the walk) or a `# gazelle:ts_ignore` in
that directory's own BUILD file. `ts_exclude` does not do it.

#### The Exclusion Report

A run says what each pattern took out of the targets it generated, one line per
pattern per package:

```
typescript: web: # gazelle:ts_exclude vite.config.ts leaves 1 TypeScript file
out of the srcs generated here: web/vite.config.ts. It names no path, so it
matches that basename at every depth below this directory; "./vite.config.ts"
anchors it here.
```

The count is how a pattern matching more than it meant to gets caught. The line
is bounded (three names and a count for the rest), and a pattern that matched
nothing in a package says nothing there, so a root-level `*.generated.ts` is
quiet everywhere it does not apply.

A bare pattern gets one line per package, because it reaches every package below
the declaration: under the default `every-dir` mode a namesake in `web/sub` is a
package of its own and is reported in its own line there. Under a rolled-up
boundary `web` claims the subtree, and one line carries the whole count:

```
typescript: web: # gazelle:ts_exclude vite.config.ts leaves 2 TypeScript files
out of the srcs generated here: web/sub/vite.config.ts, web/vite.config.ts. It
names no path, so it matches that basename at every depth below this directory;
"./vite.config.ts" anchors it here.
```

Directives are inherited, so the package a drop fires in is usually not the
package holding the line to edit. The line names the declaring build file when
those differ, and spells the anchored form that, written in that build file,
names the package the drop fired in:

```
typescript: web: # gazelle:ts_exclude *.gen.ts, declared in the workspace root,
leaves 1 TypeScript file out of the srcs generated here: web/mod.gen.ts. It names
no path, so it matches that basename at every depth below the workspace root;
"./web/*.gen.ts" in that build file matches web's own files only.
```

The claim is about the srcs of that run and no further. Exclusion happens at
generation time and never sees the merge, and `rule.MergeList` keeps a list
element carrying `# keep`, so a hand-kept `srcs` entry goes on compiling an
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

#### `ts_exclude_dir`

`ts_exclude` drops files from the targets of packages Gazelle still walks.
`ts_exclude_dir` keeps it out of a directory entirely, by basename:

```python
# BUILD.bazel (repo root)

# gazelle:ts_exclude_dir coverage
# gazelle:ts_exclude_dir storybook-static
```

Nothing is generated in any `coverage/` or `storybook-static/` below the root,
on top of the built-in `.next`, `.nuxt`, `.svelte-kit`, `dist`, `build` and
`node_modules`. The directive goes in an ancestor, because the directory it
names has no build file of its own to carry it. `ts_ignore` is for a directory
whose BUILD file you are keeping anyway.

The value is a basename, and the whole value is one name. A path, a glob or a
list of names is refused out loud, because the traversal only ever compares one
directory basename against it. `ts_exclude` does not reach a directory either:
its patterns drop files from a target's `srcs`, and under the default
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
