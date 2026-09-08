# Gazelle Directives Reference

The TypeScript extension declares no directive: `KnownDirectives()` is empty. A
package is a directory whose `tsconfig.json` lists a first-party file, its
program is that listing, and its deps come from the listing's edges, the
lockfile and the nearest `package.json` ([Overview](overview.md)). What a BUILD
file says to Gazelle is core Gazelle's own three comments, `# keep`,
`# gazelle:exclude` and `# gazelle:resolve`; everything else is the tsconfig's,
the lockfile's or the manifest's to say.

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
is replaced unless a `# keep` holds it:

| Rule | Attributes Gazelle owns |
|------|-------------------------|
| `ts_compile` | `srcs`, `deps`, `tsconfig`, `visibility` |
| `ts_test` | `srcs`, `deps`, `tsconfig`, `config`, `wrangler_config`, `coverage_provider` |
| `ts_config` | `src`, `deps`, `visibility` |
| `ts_lint` | `srcs`, `linter`, `linter_binary`, `config`, `fail_on_warnings` |
| `filegroup(name = "vitest_config")` | `srcs`, `visibility` |
| `filegroup(name = "wrangler_config")` | `srcs`, `visibility` |

`ts_config.deps` is the `extends` chain, a dep on the `ts_config` of every
`tsconfig.json` the file extends by a relative path. A base of another name
gets no label, and for an owned attribute that means the value goes: a
hand-written `deps` needs a `# keep` on its line to survive the next run. See
[the tsconfig and its ts_config](overview.md#the-tsconfig-and-its-ts_config).

!!! note "Upgrading"
    `ts_config.deps` used to be write-once; it is Gazelle's now. On every
    `ts_config` whose `deps` you wrote by hand, put `# keep` on the entry's own
    line to hold that entry beside what Gazelle computes, or above the
    attribute to keep the whole value. Without one, `deps` is recomputed entry
    by entry and each dropped entry is named in the log.

A tsconfig's `paths` and `types` are the rule's to read, so no attribute
restates them. What a path-shaped `types` entry asks of Gazelle is the dep on
the target that stages the file it names, written into `deps` like any other;
see [a declaration the tsconfig names](overview.md#a-declaration-the-tsconfig-names).

`ts_codegen`, `ts_dev_server`, `ts_pnpm` and `ts_add_package` are outside all
of this: Gazelle writes none of them and touches none, so each comes through
every run as written, load symbol included, `# keep` or not. A `ts_codegen` is
read, for its `outs` and `out_dir`, and never rewritten.

A directory that is not a package has every rule Gazelle would write there
withdrawn, under the names it would use; `# keep` above such a rule holds it.

`# keep` works at three granularities: one value, one attribute, one rule:

```python
ts_compile(
    name = "app",
    srcs = [
        "main.ts",
        "legacy.js",  # keep
    ],
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
itself or an edit of yours. `deps` is the exception: it is filled in after
resolution, when the value on disk is no longer in hand, so a label you wrote
into it goes without a report unless `# keep` holds it.
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

## `# gazelle:exclude`

Core Gazelle's, and the one way to keep Gazelle out of a tree.
`# gazelle:exclude <path>` in a BUILD file prunes the walk below that path, so
no BUILD file is written there and nothing under it is visited; `.bazelignore`
does the same for Bazel and Gazelle alike, and `# gazelle:ignore` in a
directory's own BUILD file leaves that file untouched. A file a program lists
under a pruned directory belongs to no package and is named in the log with
the programs that reached it. What a package compiles is its tsconfig's
`include`, `files` and `exclude`; what Bazel must not enter -- `node_modules`,
a generated directory, a fixture workspace with a `MODULE.bazel` of its own --
is excluded here.

```python
# BUILD.bazel (repo root)
# gazelle:exclude examples
```

## `# gazelle:resolve`

Core Gazelle's override of one resolution:
`# gazelle:resolve typescript <repository path> <label>`. The import string is
the repository path of the file tsgo listed as the edge's target, not the
specifier a source wrote, because `deps` is resolved by the file; the label is
what every edge to that file becomes, ahead of the rule whose `srcs` hold it.
It is the escape hatch for a first-party file no generated rule owns: a
declaration under a directory Gazelle does not walk, a file a hand-written rule
stages under another name. The directive is inherited by the directories below
the BUILD file that carries it, as every core directive is.

```python
# BUILD.bazel (repo root)
# gazelle:resolve typescript vendor/legacy/index.d.ts //vendor/legacy
```

## Removed Directives

!!! note "Upgrading"
    Every `ts_*` directive line in a BUILD file goes: the extension declares
    none, and core Gazelle reports an unknown directive and continues. The
    table names what does each one's job.

| Directive | What does its job |
|-----------|-------------------|
| `# gazelle:ts_package_boundary` | the `tsconfig.json`: a directory is a package when its program lists a first-party file, and everything the program lists belongs to it |
| `# gazelle:ts_exclude` | the tsconfig's `include`, `files` and `exclude`; a tree Bazel must not enter is `# gazelle:exclude` or `.bazelignore` |
| `# gazelle:ts_exclude_dir` | `# gazelle:exclude` |
| `# gazelle:ts_ignore` | `# gazelle:ignore`, or no `tsconfig.json` |
| `# gazelle:ts_target_name` | a hand-written rule under the name wanted; Gazelle writes and withdraws only the names it would generate |
| `# gazelle:ts_js_srcs` | `allowJs` in the tsconfig: tsgo lists the `.mjs`, and it is a src |
| `# gazelle:ts_ambient_types` | `compilerOptions.types`, or a `/// <reference types>` directive; both are edges in the listing |
| `# gazelle:ts_runtime_dep` | the package's `package.json`: a `ts_test`'s deps carry its `dependencies` and `devDependencies` |
| `# gazelle:ts_warn_unresolved` | tsgo prints no line for a specifier it could not resolve; a run over an island prints them from `--traceResolution`, and the type check reports `TS2307` |
| `# gazelle:ts_declarations` | the flag `--//ts:declarations`, one value for the whole build |
| `# gazelle:ts_path_alias` | `compilerOptions.paths`, read by tsgo when it lists the program and by the rule when it checks it |
| `# gazelle:ts_codegen` | a hand-written `ts_codegen`; Gazelle indexes its `outs` and `out_dir` and never writes one |
| `# gazelle:ts_npm_hub` | every npm label Gazelle writes names `@npm`, the hub of the root lockfile it reads; a package on another hub writes its deps by hand under `# keep` |
| `# gazelle:ts_npm_mapping` | the lockfile is the inventory; a package from a label of your own is a `# keep` dep |
| `# gazelle:ts_asset_declaration_type` | the tsconfig or a declaration file in `srcs`: `vite/client` in `types`, or a `declare module "*.svg"` |
