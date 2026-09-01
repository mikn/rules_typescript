# Gazelle Directives Reference

Directives go in `BUILD.bazel` files as comments and control how Gazelle generates BUILD rules for that directory and its children.

## Full Reference

| Directive | Effect |
|-----------|--------|
| `# gazelle:ts_declarations oxc` | Emit `declarations = "oxc"` on generated `ts_compile` and `ts_test` rules in this tree — syntactic `.d.ts` emit, so every export needs an explicit type |
| `# gazelle:ts_declarations tsgo` | Return a subdirectory to the default emitter after a parent set `oxc` |
| `# gazelle:ts_package_boundary every-dir` | (default) Every directory with `.ts` files becomes a package |
| `# gazelle:ts_package_boundary index-only` | Only directories with `index.ts`/`.tsx` become packages (pre-0.2.0 behaviour) |
| `# gazelle:ts_package_boundary tsconfig` | Only directories holding a `tsconfig.json` become packages, so one target covers one TypeScript project |
| `# gazelle:ts_package_boundary true` | Mark this single directory as a boundary (useful in index-only mode without `index.ts`) |
| `# gazelle:ts_ignore` | Suppress TypeScript rule generation for this directory and its children |
| `# gazelle:ts_ignore false` | Re-enable generation after a parent used `ts_ignore` |
| `# gazelle:ts_target_name my_lib` | Override the default target name (which is the directory basename) |
| `# gazelle:ts_path_alias @/ src/` | Map a TypeScript path alias to a workspace-relative directory |
| `# gazelle:ts_runtime_dep @npm//:happy-dom` | Append a label to every generated `ts_test` deps list |
| `# gazelle:ts_ambient_types @npm//:types_node` | Append a label to every generated `ts_compile` and `ts_test` deps list |
| `# gazelle:ts_exclude *.generated.ts` | Exclude files matching this pattern from source targets |
| `# gazelle:ts_warn_unresolved true` | Warn when an import cannot be resolved to a Bazel label |
| `# gazelle:ts_codegen <name> <generator> <outs> [srcs:<csv>] [args…]` | Register a `ts_codegen` target in this directory; a `srcs:` entry may be a `glob()` call |
| `# gazelle:ts_npm_hub npm_eslint` | Resolve bare specifiers in this tree into that npm hub repo, not the default `@npm` |
| `# gazelle:ts_asset_declaration_type .svg <type>` | What an import of that asset extension resolves to in this tree, written into every `asset_library`'s `declaration_type` |

That is the complete set: twelve directives. Gazelle warns on an unknown
`# gazelle:ts_*` comment and continues, so a typo shows up in the run output.

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
| `ts_config` | `src`, `visibility` — `deps`, the `extends` chain, is yours |
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
| `filegroup(name = "sources")` | `srcs`, `visibility` |

`ts_compile.public_globals` is deliberately absent. Whether a `.d.ts`'s globals
are part of the package's public type surface is a decision nothing in the
source states, so no directive writes it and a hand-written value survives every
run, `# keep` or not.

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

### Index-only package boundaries (pre-0.2.0 behaviour)

```python
# BUILD.bazel (repo root)

# gazelle:ts_package_boundary index-only
```

In this mode a directory without an index file is not a package: its sources roll
up into the nearest ancestor that is one, and no BUILD file is written there. That
dissolves the commonest package-level cycle, where a barrel re-exports `./rules`
while `./rules` imports `../utils`: a cycle between two Bazel packages and none
between files.

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
  at directory granularity there is one.

### More than One npm Hub

A workspace can translate several lockfiles, and which hub a package's imports
come from is a property of that package, so it is named where the package is:

```python
# eslint-plugin/BUILD.bazel

# gazelle:ts_npm_hub npm_eslint
```

Generated deps in that tree then read `@npm_eslint//:eslint` rather than
`@npm//:eslint`. Without it the label names a hub the package does not use, and
that label does not exist. Both `npm_eslint` and `@npm_eslint` are accepted.

Declaring a hub is `npm.translate_lock(name = ...)` plus a matching `use_repo`,
and each hub needs its own `ts_add_package` target. See
[More than one hub](../guides/npm.md#more-than-one-hub).

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
`ts_compile` still names only direct `@types/*` deps in the tsconfig's `files`.
See [ts_compile](../rules/ts-compile.md).

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
the tree under a rolled-up boundary (`# gazelle:ts_package_boundary index-only`
or `tsconfig`) where a subdirectory is not a package to begin with.

### Exclude Generated Files

```python
# src/graphql/BUILD.bazel

# gazelle:ts_exclude *.generated.ts
```

Files matching `*.generated.ts` are excluded from `srcs` lists in this directory.
Nothing is excluded by name on its own: a checked-in file is a source unless a
rule in the package declares it as an output, however it is named.
