# IDE Setup

`ts_refresh_tsconfig` turns Bazel's build graph into the two things an editor can
read:

1. A workspace-root `tsconfig.json` whose `compilerOptions.paths` names every
   source root, path alias, `module_name` and npm package your targets reach.
   This is the primary mechanism, and the file is meant to be checked in.
2. A **tsserver plugin** that resolves the same set live, following `bazel build`
   outputs with no tsconfig reload. It is a layer on top of the generated file,
   and it needs editor configuration; the generated file needs none.

## Setup

Declare the target once, in your root `BUILD.bazel`:

```python
load("@rules_typescript//ts:defs.bzl", "ts_refresh_tsconfig")

ts_refresh_tsconfig(
    name = "refresh_tsconfig",
    test = True,
    deps = [
        "//apps/web",
        "//packages/ui",
    ],
)
```

An aspect walks `deps` from each entry, so listing a target covers everything it
depends on. Two constraints:

- **`deps = []` is the attribute default, and it reaches nothing.** No packages,
  no aliases, no npm entries: a `tsconfig.json` with an empty `paths`, and an
  editor told nothing.
- **`deps` obeys visibility**, so a package-private `ts_compile` target cannot be
  listed here. Gazelle writes `visibility = ["//visibility:public"]` on the
  targets it generates, and so does `ts_test` for the `ts_compile` targets it
  generates from `srcs`, `setup_files` and `global_setup`: `//path:_my_test_compile`
  is listable, and the npm packages only a test declares reach the tsconfig.
  `visibility` on the `ts_test` narrows them again, and the generated targets
  follow it. Hand-written private targets are covered by
  [Complete coverage for the resolution map](#complete-coverage-for-the-resolution-map).

Then run it:

```bash
bazel run //:refresh_tsconfig
```

That writes, into the source tree:

| Path | What it is |
|---|---|
| `tsconfig.json` | Compiler options and the `paths` map. **Check this in** |
| `.bazel/npm/` | The `.d.ts` (and `package.json`) of every npm package the `paths` entries name, plus a `.gitignore` of `*` |
| `.bazel/tsserver-hook-data.json` | The same graph facts, in the shape the plugin reads |
| `.bazel/node_modules/@rules_typescript/tsserver-plugin/` | The tsserver plugin, as a package tsserver can load by name |
| `.bazel/tsserver-hook.js` | A preload variant for a client that resolves through the public `ts.resolveModuleName`; see [What the preload does not reach](#what-the-preload-does-not-reach) |
| `.bazel/tsserver-hook-resolver.js` | The map builder both front-ends share |
| `.bazel/tsserver-hook-worker.js` | Its background worker |

Two attributes move the first two. `tsconfig` (default `"tsconfig.json"`) is
where the generated config lands. `npm_dir` (default `".bazel/npm"`) is where the
npm declarations land; `npm_dir = ""` opts out, dropping the npm `paths` entries
and their files for a workspace that resolves npm types some other way.

!!! warning "It replaces the file at `tsconfig` wholesale"
    A migrating repository already has a root `tsconfig.json`, and the first
    `bazel run //:refresh_tsconfig` overwrites it: `include`, `baseUrl`,
    `module` and every other option in it, not only `paths`. The generated file
    is a complete config and carries nothing over from yours.

    Move your own options before that first run. Keep them in a file of another
    name and name that in `ts_compile(tsconfig = ...)`, which is what the compile
    actions read
    ([where compiler options come from](../rules/ts-compile.md#where-compiler-options-come-from)).
    Pointing `ts_compile` at the generated file instead makes it that target's
    own baseline. Or set `ts_refresh_tsconfig(tsconfig = "tsconfig.bazel.json")`
    and `extends` the generated file from yours.

### Excluding Foreign TypeScript

The generated config leaves `include` at `**/*`, so `tsc` walks every `.ts` in
the repository, including trees outside this module's build graph: a nested Bazel
module, a workspace listed in `.bazelignore`, a vendored example. Nothing in
`deps` names those files, so they are checked under the wrong `compilerOptions`
and their errors are noise. `extra_exclude` adds globs to the generated
`exclude`:

```python
ts_refresh_tsconfig(
    name = "refresh_tsconfig",
    test = True,
    extra_exclude = ["**/e2e", "**/examples"],
    deps = ["//apps/web", "//packages/ui"],
)
```

Anchor each entry with `**/`, the way the built-in exclusions
(`**/bazel-*`, `**/node_modules`, `**/dist`, `**/build`, `**/.next`,
`**/.nuxt`, `.bazel`) are.

### Nested Tsconfigs

One `compilerOptions` block cannot serve a target that turns `strict` off beside
one that leaves it on, or a target naming a `lib` its `target` does not imply.
An editor resolves a file to a program **by directory**, so such a package needs
its own `tsconfig.json` next to its sources, and the root has to stop claiming
those files.

`ts_refresh_tsconfig` generates those files, and you declare which packages get
one:

```python
ts_refresh_tsconfig(
    name = "refresh_tsconfig",
    test = True,
    nested_tsconfigs = ["apps/worker/tsconfig.json"],
    deps = ["//apps/web", "//apps/worker"],
)
```

The set is computed by comparing each target's options against the root block.
Two details affect that comparison:

- **`target` and `jsx_mode` count.** They are rule attributes and not
  `compiler_options` entries. A target setting either to something other than the
  root's value (`ES2022`, `react-jsx`) goes on the list.
- **Values are canonicalised before they are compared.** TypeScript reads
  `target`, `module`, `moduleResolution`, `jsx`, `moduleDetection` and `newLine`
  case-insensitively and treats `lib` as a set, so `"Preserve"` and `"preserve"`
  are not a disagreement and neither is `["esnext", "dom"]` against
  `["DOM", "ESNext"]`. Folding both sides keeps a package that merely restates a
  default off the list, and keeps two targets spelling one value differently from
  reading as a conflict.

The rule **fails when the declared list disagrees with the graph**, in either
direction, and the message names what to add or remove. The list is declared
because `glob()` does not cross a package boundary, and a leftover entry would go
on owning its subtree in the editor. Each entry gets its own staleness
`diff_test`.

Each generated file `extends` the root **and** the package's own `ts_compile`
baseline, root first so the baseline wins. Inherited `paths` are not re-resolved,
so the root's aliases still work from down there; `include` and `exclude` are
re-resolved against the extending file, so they are written out. `noEmit`,
`composite`, `incremental`, `rootDir` and `files` are pinned in the file itself,
since a baseline inherited whole would emit into your source tree, reject files
outside one target's `rootDir`, and lose every ambient declaration.

A package whose targets set **the same** option to **different** values has no
representation, since one directory cannot hold both answers. That is an error
naming both targets; move one target into its own package.

**Two different `tsconfig` baselines in one package is the same error.**
TypeScript applies an `extends` array later-wins, so listing both baselines would
let one's keys replace the other's for both targets' sources. A package gets at
most one baseline, from whichever of its targets name one.

A target in that package naming **no** `tsconfig` inherits that baseline in the
editor, and does not in the build: the rule applies its own baseline options
(`strict`, `module: Preserve`, `moduleResolution: Bundler`, `skipLibCheck`,
`esModuleInterop`) in either mode, and with no `tsconfig` above them that is all
it gets — which is what the root block already holds.
The nested file's own `compilerOptions` restate every option any target in the
package sets explicitly and beat every `extends`, so a baseline reaches only keys
no target in the package has an opinion about. In `//vite`, `:plugin_typecheck`
names `vite.tsconfig.json` and `:tsup_config` names nothing, and the generated
`vite/tsconfig.json` pins the `module`/`moduleResolution` both targets ask for,
keeping the baseline's `Node16` answer away from `tsup.config.ts`. Give the odd
target the same `tsconfig`, or its own package, when that is not close enough.

### Bare Specifiers for First-Party Packages

A target that sets `module_name = "@acme/ui"` gets an `@acme/ui/*` `paths` entry
of its own, and `@acme/ui` too once the package has an index file, so the editor
resolves the same bare specifier `ts_compile` resolves during the build. Those
keys are written last, so a first-party `module_name` wins over a same-named npm
package, the precedence `ts_compile`'s own generated tsconfig uses.

`module_name` also covers a pnpm `link:`/`workspace:` dependency imported by its
package name. Bazel resolves the hub's alias before Starlark sees it, so the name
the code imports exists only inside the alias; `module_name` on the target that
produces the declarations puts it back in the graph.

### npm Declarations

Each npm package is its own lazily-fetched Bazel repository, living only under
`<output_base>/external/`, which nothing links into the execroot the
`bazel-<workspace>` symlink points at, so no workspace-relative path reaches it.
Copying the `.d.ts` into `npm_dir` makes a `paths` entry possible, and the copies
are keyed by package name, so the canonical repository name that changes on every
version bump never enters the config.

The copied `.d.ts` is the entry point the package's own metadata designates. See
[how that is resolved](../guides/npm.md#where-a-packages-type-declarations-come-from).
The wildcard entry lists the package root and then that file's directory, in the
order npm would look: with no `exports` map — which is most of the registry —
`pkg/sub` is a plain path under the package root, and a package whose subpaths do
sit beside its entry is answered by the second substitution.

```json
"vite":   ["./.bazel/npm/vite/dist/node/index.d.ts"],
"vite/*": ["./.bazel/npm/vite/*", "./.bazel/npm/vite/dist/node/*"]
```

A `@types/*` package is keyed by the name it types rather than its own, which is
the only specifier anything imports it by, and installs under its own name, which
the key points into:

```json
"estree":   ["./.bazel/npm/@types/estree/index.d.ts"],
"estree/*": ["./.bazel/npm/@types/estree/*"]
```

Which of the two names wins follows npm, the same way it does in the tsconfig
`ts_compile` generates: the runtime package answers `x` when it publishes
declarations of its own, `@types/x` when it publishes none, and a `path_aliases`
prefix outranks both. Where two packages in the graph claim one key — a target
whose closure holds `@types/x` and no `x`, beside a target that has the real `x`
— the same rule picks, so the aggregate config agrees with each target's own.

That key is the only route a `@types/*` package reached **transitively** has.
The other route is `files`, which carries the globals such a package declares
([Ambient Types in the Editor](#ambient-types-in-the-editor)) — but `files` is
built from what each reached target names in its own `deps`, and `from "estree"`
is usually written in a dependency's `.d.ts` rather than in your sources. So
`@types/estree` behind `rollup` gets a `paths` key and no `files` entry, while a
`@types/node` you depend on directly gets both, naming one installed copy.

### Staleness Test

`test = True` adds a `diff_test` named `<name>_test` that compares the
checked-in `tsconfig.json` against the one the graph currently implies:

```bash
bazel test //:refresh_tsconfig_test
```

It fails whenever a dependency edit changes what the IDE should see:

```
tsconfig.json is stale: run `bazel run //:refresh_tsconfig`.
```

Turn it on once the file is checked in.

## Complete Coverage for the Resolution Map

`deps` is a rule attribute, so it reaches only what this workspace's visibility
lets a rule name. An **aspect** propagates along the dependency edges a build
already has and creates none, so it needs no grant. Two lines in `.bazelrc` turn
that on for every build:

```
build --aspects=@rules_typescript//ts/private:tsconfig_aspect.bzl%tsconfig_aspect
build --output_groups=+ide_fragments
```

Every target whose closure holds a source root, a path alias or an npm entry
then gets a `<target>.tsconfig-fragment.json` beside its other outputs in
`bazel-out`, and the resolver merges what it finds there into the map. `+group` is
additive, so this composes with `--output_groups=+_validation` and with anything
a command line adds, and any ordinary `bazel build` refreshes the fragments.

**Both lines are optional.** Without them nothing writes fragments and the plugin
works from `.bazel/tsserver-hook-data.json` alone. Fragments augment that file;
they never replace it, and every key it resolved wins over a fragment that
disagrees.

### What Fragments Cover

| | Covered by | Reaches package-private targets |
|---|---|---|
| `ts_compile` source roots | fragments, and the data file | yes, via fragments |
| `module_name` bare specifiers | fragments, and the data file | yes, via fragments |
| `ts_path_alias` prefixes | fragments, the data file, and a BUILD-file scan | yes, via fragments |
| npm `.d.ts` declarations | `.bazel/npm`, installed by `bazel run //:refresh_tsconfig` | **no** |

The npm row is the exception for the same reason `.bazel/npm` exists: a fragment
can only name the package, since nothing in the external repository has a
workspace-relative path. Whether that name resolves depends on what
`bazel run //:refresh_tsconfig` last installed, and that target's `deps` do obey
visibility.

The checked-in `tsconfig.json` does not change either. It stays what
`refresh_tsconfig` generates from `deps`, which is what a fresh clone, a plain
`tsc` run and every editor read. Fragments reach the plugin only.

### Cost

- **Each fragment carries its target's whole closure**, at the cost of bytes:
  any one fragment is a complete answer for its own subgraph, which makes a
  partially built `bazel-out` usable.
- **A deleted or renamed target leaves its fragment behind**, because nothing
  cleans `bazel-out`. The resolver opens fragments only under directories the source
  tree still has a BUILD file for, and nothing enters the map unless the path it
  names exists on disk, so a stale fragment contributes nothing. `bazel clean` is
  not needed.

## Ambient Types in the Editor

The editor is more permissive than the build in one place. `ts_compile` names a
target's **direct** `@types/*` deps in the tsconfig it gives tsgo, plus what
their entries name in `/// <reference types=...>` (`@types/bun` forwards to
`bun-types`, whose entry references `node`), so a global reaches a target
because that target asked for it, or for a package whose entry references it.
The editor program has one root `compilerOptions` block for the whole workspace,
and its `files` array is the **union** of what every reached target declares —
each target's own direct `@types/*` deps and what those reference, pooled. A
file using `process` therefore type-checks in the editor as soon as *some*
target in the graph declared `@types/node`, and then fails `bazel build` with
the strict-deps error naming the label to add.

The union is over direct deps and what their entries reference, so a `@types/*`
package reached only through an import — `from "estree"` in a dependency's own
`.d.ts` — is named in `files` nowhere, in either config. It still resolves as a
module, through the `paths` key it takes under the name it types
([npm Declarations](#npm-declarations)); `files` is what it is not in.

Narrowing the union per target would need a tsconfig per target, and a package
only gets its own program when its `compilerOptions` genuinely disagree with the
root ([`nested_tsconfigs`](#nested-tsconfigs)). Narrowing it globally would
make the editor wrong for every target that does declare the dep.

Treat `bazel build` as the authority, and declare ambient packages up front.
[`# gazelle:ts_ambient_types`](../gazelle/directives.md#declare-ambient-types-once-for-the-whole-repo)
does that for a whole tree in one line.

## Editor Configuration

The generated `tsconfig.json` needs no editor setup; every editor already reads
it, and it is what makes a Bazel-built declaration resolve in a fresh clone.
The rest of this section is for the plugin, which adds live resolution on top.

tsserver loads a plugin by name from a probe location. The plugin is installed as
a package under `.bazel/node_modules/`, so the probe location is `.bazel` and the
name is `@rules_typescript/tsserver-plugin`. Every recipe below is those two
facts in one editor's spelling.

### VS Code

`.vscode/settings.json`:

```json
{
  "typescript.tsserver.pluginPaths": [".bazel"]
}
```

That is the whole of it. VS Code passes no `--globalPlugins`, so the plugin has
to be named in the config the editor is using as well — but the generated
`tsconfig.json` already names it, and `bazel run //:refresh_tsconfig` keeps it
there. Do not add the entry by hand to a config that macro owns: the next
refresh rewrites the file whole and drops it, and tsserver logs and ignores a
plugin it cannot load, so the only symptom is imports quietly going unresolved
again.

A workspace whose editor config is its own file — `ts_refresh_tsconfig(tsconfig
= "tsconfig.bazel.json")` with a hand-written `tsconfig.json` that `extends` it
— inherits the entry through `extends` and needs nothing either.

Restart the TS server: `Cmd+Shift+P` → `TypeScript: Restart TS Server`.

### Neovim (nvim-lspconfig with typescript-language-server)

```lua
require('lspconfig').ts_ls.setup({
  init_options = {
    plugins = {
      { name = "@rules_typescript/tsserver-plugin", location = ".bazel" },
    },
  },
})
```

`typescript-language-server` turns its `plugins` option into tsserver's
`--globalPlugins` and `--pluginProbeLocations`, so no `compilerOptions.plugins`
entry is needed with it.

### Neovim (coc-tsserver)

`coc-settings.json`:

```json
{
  "tsserver.globalPlugins": [
    { "name": "@rules_typescript/tsserver-plugin", "location": ".bazel" }
  ]
}
```

### Emacs (lsp-mode)

```elisp
(setq lsp-clients-typescript-plugins
  (vector (list :name "@rules_typescript/tsserver-plugin"
                :location ".bazel")))
```

### tsserver Directly

Any client that spawns tsserver itself takes the two flags:

```
--globalPlugins @rules_typescript/tsserver-plugin
--pluginProbeLocations /abs/path/to/workspace/.bazel
```

## Coding Agent Harnesses

A coding agent that reads TypeScript usually runs a language server of its own
rather than an editor's. Claude Code, for example, installs
`typescript-language-server` and `typescript`. The short answer for those:

**The generated `tsconfig.json` works with no configuration at all.** It is a
checked-in file with a `paths` map, which is the mechanism every TypeScript tool
already reads. An agent's language server resolves a Bazel-built `.d.ts` through
it without knowing Bazel exists, and resolves it to the real declarations rather
than to `any` — a nonexistent member on an imported symbol is still an error.
Keep the file current with `bazel run //:refresh_tsconfig`, which the
[staleness test](#staleness-test) will ask for.

**The plugin needs the harness to let you configure the server**, which is the
part that varies. If the harness exposes LSP `initializationOptions`, pass the
`plugins` entry from the
[nvim-lspconfig recipe](#neovim-nvim-lspconfig-with-typescript-language-server) —
`typescript-language-server` is what most of them run. If it does not, the plugin
cannot be reached and the `tsconfig.json` is the whole answer.

Two things to know before reaching for a generic mechanism:

- **`NODE_OPTIONS` is not a way in.** It propagates into the forked tsserver, so
  the preload does load there, but loading is not the same as taking effect; see
  [What the preload does not reach](#what-the-preload-does-not-reach).
- **A relative path in `NODE_OPTIONS` is worse than useless.** Node resolves
  `--require ./x.js` against the process's cwd, and from any other directory the
  process fails to start at all — `Cannot find module`, with
  `requireStack: [ 'internal/preload' ]`, exit 1. An agent's language server
  would die rather than degrade. Absolute paths only.

To check what your harness actually gives you, ask its language server for
diagnostics on a file importing a Bazel-built package. `TS2307 Cannot find
module` before `bazel run //:refresh_tsconfig` and no diagnostic after it means
the `tsconfig.json` path works. `TSSERVER_HOOK_DEBUG=1` in the server's
environment makes the plugin report on its stderr whether it loaded and how many
entries its map holds.

### What the Preload Does Not Reach

`.bazel/tsserver-hook.js` patches the `typescript` module's exported
`resolveModuleName`. That reaches a client which builds a `LanguageService` host
itself and routes resolution through the public API. It does **not** reach a
standalone tsserver process, and every editor above spawns one.

Two measured facts, in case the distinction matters to you. `lib/tsserver.js`
loads its bundle as `require("./typescript.js")`, which the preload's matcher
does not accept, so the patch never installs. Widen the matcher so it does
install, and a real tsserver still reports `TS2307` for the same import: the
language service resolves through its `LanguageServiceHost`, not through the
export. Decorating that host is what the plugin does, and it is why the plugin
exists.

## How It Works

The plugin is TypeScript's equivalent of Go's
[GOPACKAGESDRIVER](https://jayconrod.com/posts/125/go-editor-support-in-bazel-workspaces),
with one difference: **it never runs Bazel**. Everything Bazel knows arrives
through `.bazel/tsserver-hook-data.json`, which `refresh_tsconfig` wrote at
analysis time. A long-lived editor process asking the Bazel server for anything
would sit on the same lock a build wants.

1. **Worker thread** reads `.bazel/tsserver-hook-data.json` — the npm entry
   points, the `ts_compile` package list, the `module_name` specifiers, the path
   aliases — and turns it into a module-name → declaration-path map
2. **npm packages** resolved from the declarations installed under `npm_dir`,
   the same set the generated `tsconfig.json` names
3. **Internal packages** resolved from `bazel-bin` (`.d.ts` after a build) or the
   source tree (`.ts` before one)
4. **Fragments**, if the `.bazelrc` lines above are in place, add the packages and
   aliases of every target the aspect reached, including the ones no rule may
   name. One target built in two configurations writes two fragments, deduplicated
   by label with the first config root in sorted order winning, so the merge does
   not depend on what `bazel-out` holds
5. **Path aliases** come from that graph data, plus a scan of
   `# gazelle:ts_path_alias` directives in BUILD files to cover directives added
   since the last refresh. The graph wins, since it is what the build resolves
6. **File watcher** watches the graph data file, the root `BUILD.bazel` and
   `pnpm-lock.yaml`, and `bazel-bin` recursively for new `.d.ts` and new
   fragments; a change to any of them rebuilds the map

The main thread is never blocked: the worker builds the map off-thread and posts
it back. tsserver returns "unresolved" briefly on first load, then resolves when
the worker completes.

### Resolution Priority

1. `.d.ts` in `bazel-bin` — fast, precise (available after `bazel build`)
2. `.ts` source file — always available, slower for tsserver to process
3. npm declarations under `npm_dir` — whatever the last
   `bazel run //:refresh_tsconfig` installed

### What a Build Provides

First-party resolution works without `bazel build`, since the source `.ts` files
are always on disk. A build adds the `.d.ts` files and, with the aspect enabled,
the fragments naming the packages `deps` could not reach.

npm resolution is bounded by the refresh. The packages that resolve are the ones
reachable from `deps` when `refresh_tsconfig` last ran, in both the `paths`
entries and the plugin, so an import pulling in a package none of those targets
reached is unknown until you re-run the target. The staleness test asks for that
same re-run.

## Debugging

Set `TSSERVER_HOOK_DEBUG=1` in the environment the language server starts in.
The plugin and its worker then report on the server process's stderr: whether the
plugin loaded, which project it decorated, how many entries the map holds, and
each invalidation. It is the server's stderr and not the tsserver log, so where
it surfaces depends on the client.

## Debugging Tests in vs Code

To attach a debugger to vitest running inside the Bazel sandbox:

```python
ts_test(
    name = "my_test_debug",
    srcs = ["my.test.ts"],
    deps = [":my_lib", "@npm//:vitest"],
    tags = ["manual"],
    env = {"NODE_OPTIONS": "--inspect-brk=9229"},
)
```

```bash
bazel run //path/to:my_test_debug
```

Vitest pauses before executing, waiting for a debugger on port 9229. Attach VS Code via "Attach to Node Process" or use `chrome://inspect`. Source maps are configured automatically.
