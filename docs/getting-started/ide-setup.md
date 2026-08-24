# IDE Setup

`ts_refresh_tsconfig` turns Bazel's build graph into the two things an editor can
read:

1. A workspace-root `tsconfig.json` whose `compilerOptions.paths` names every
   source root, path alias, `module_name` and npm package your targets reach.
   **This is the primary mechanism**, and the file is meant to be checked in.
2. A **tsserver hook** that resolves the same set live, for editors that would
   rather follow a `bazel build` than reload a tsconfig. It is a layer on top,
   not a replacement.

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

`deps` carries the whole thing. An aspect walks `deps` from each entry, so
listing a target covers everything it depends on. Two things to know before
writing the list:

- **`deps = []` is the attribute default, and it reaches nothing.** No packages,
  no aliases, no npm entries — a `tsconfig.json` with an empty `paths`. A
  `ts_refresh_tsconfig` with no `deps` runs fine and tells your editor nothing.
- **`deps` obeys visibility**, like any rule's. A package-private `ts_compile`
  target cannot be listed here. Gazelle writes
  `visibility = ["//visibility:public"]` on the targets it generates, so a
  workspace whose BUILD files come from Gazelle can list any of them — and
  `ts_test` does the same for the `ts_compile` targets it generates from your
  `srcs`, `setup_files` and `global_setup`, so `//path:_my_test_compile` is
  listable and the npm packages only a test declares reach the tsconfig. (Set
  `visibility` on the `ts_test` to narrow them again; the generated targets
  follow it.) For the hand-written private ones,
  [Complete coverage for the hook](#complete-coverage-for-the-hook) covers them
  without a grant.

Then run it:

```bash
bazel run //:refresh_tsconfig
```

That writes, into the source tree:

| Path | What it is |
|---|---|
| `tsconfig.json` | Compiler options and the `paths` map. **Check this in** |
| `.bazel/npm/` | The `.d.ts` (and `package.json`) of every npm package the `paths` entries name, plus a `.gitignore` of `*` |
| `.bazel/tsserver-hook-data.json` | The same graph facts, in the shape the hook reads |
| `.bazel/tsserver-hook.js` | The resolution hook |
| `.bazel/tsserver-hook-worker.js` | Its background worker |

Two attributes move the first two. `tsconfig` (default `"tsconfig.json"`) is
where the generated config lands. `npm_dir` (default `".bazel/npm"`) is where
the npm declarations land; `npm_dir = ""` opts out, dropping the npm `paths`
entries and their files for a workspace that resolves npm types some other way.

### Keeping foreign TypeScript out of the program

The generated config sets `include: ["**/*"]`, so `tsc` walks every `.ts` in the
repository — including trees that are not in this module's build graph at all: a
nested Bazel module, a workspace listed in `.bazelignore`, a vendored example.
Nothing in `deps` names those files, so they are checked under the wrong
`compilerOptions` and their errors are noise. `extra_exclude` adds globs to the
generated `exclude`:

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

### Bare specifiers for first-party packages

A target that sets `module_name = "@acme/ui"` gets `@acme/ui` and `@acme/ui/*`
`paths` entries of its own, so the editor resolves the same bare specifier
`ts_compile` resolves during the build. Those keys are written last, which means
a first-party `module_name` wins over a same-named npm package — the precedence
`ts_compile`'s own generated tsconfig uses.

`module_name` is also the answer for a pnpm `link:`/`workspace:` dependency
imported by its package name. Bazel resolves the hub's alias before Starlark
sees it, so the name the code imports exists only inside the alias; declaring
`module_name` on the target that produces the declarations is what puts it back
in the graph.

### Why the npm declarations get copied

Each npm package is its own lazily-fetched Bazel repository, and it exists only
under `<output_base>/external/` — which nothing links into the execroot that the
`bazel-<workspace>` symlink points at. No workspace-relative path reaches it, so
a `tsconfig.json` cannot name it. Copying the `.d.ts` into `npm_dir` is what
makes a `paths` entry possible, and it keys them by package name rather than by
a canonical repository name that changes on every version bump.

### Keeping the checked-in file honest

`test = True` adds a `diff_test` named `<name>_test` that compares the
checked-in `tsconfig.json` against the one the graph currently implies:

```bash
bazel test //:refresh_tsconfig_test
```

It fails with `tsconfig.json is stale: run 'bazel run //:refresh_tsconfig'`
whenever a dependency edit changes what the IDE should see. Turn it on once the
file is checked in.

## Complete coverage for the hook

`deps` is a rule attribute, so it reaches only what this workspace's visibility
lets a rule name. An **aspect** is not: it propagates along the dependency edges
a build already has and creates none, so it needs no grant at all. Two lines in
`.bazelrc` turn that on for every build:

```
build --aspects=@rules_typescript//ts/private:tsconfig_aspect.bzl%tsconfig_aspect
build --output_groups=+ide_fragments
```

Every target the aspect reaches then gets a `<target>.tsconfig-fragment.json`
beside its other outputs in `bazel-out`, and the hook merges what it finds there
into the map. `+group` is additive, so this composes with the
`--output_groups=+_validation` above and with anything a command line adds; no
extra command is involved, and any ordinary `bazel build` refreshes the fragments
as a side effect of building what you asked for.

**Both lines are optional.** Without them nothing writes fragments, the hook
finds none, and it works exactly as it did before they existed — from
`.bazel/tsserver-hook-data.json` alone. Fragments augment that file; they never
replace it, and every key it resolved wins over a fragment that disagrees.

### What fragments do and do not cover

| | Covered by | Reaches package-private targets |
|---|---|---|
| `ts_compile` source roots | fragments, and the data file | yes, via fragments |
| `module_name` bare specifiers | fragments, and the data file | yes, via fragments |
| `ts_path_alias` prefixes | fragments, the data file, and a BUILD-file scan | yes, via fragments |
| npm `.d.ts` declarations | `.bazel/npm`, installed by `bazel run //:refresh_tsconfig` | **no** |

The npm half is the exception, and the reason is the same one that makes
`.bazel/npm` necessary at all: an npm package's declarations live in a lazily
fetched external repository that no workspace-relative path reaches, so a
fragment can only *name* the package, not point at anything readable. Whether
that name resolves depends on what `bazel run //:refresh_tsconfig` last
installed, and that target's `deps` do obey visibility. So fragments give you the
first-party half in full and leave the npm half exactly where it was.

The checked-in `tsconfig.json` does not change either. It stays what
`refresh_tsconfig` generates from `deps`, which is what a fresh clone, a plain
`tsc` run and every non-hook editor read. Fragments are a hook-only mechanism.

### Two things to know about the cost

- **Each fragment carries its target's whole closure**, so the set is redundant
  by design: any one fragment is a complete answer for its own subgraph, which is
  what makes a partially built `bazel-out` still usable. The bytes are the price.
- **A deleted or renamed target leaves its fragment behind**, because nothing
  cleans `bazel-out`. This is handled rather than avoided. The hook looks for
  fragments only under directories the source tree still has a BUILD file for, so
  a fragment whose package is gone is never opened; and nothing enters the map
  without the path it names existing on disk, so a fragment naming a removed
  package or a renamed alias contributes nothing. `bazel clean` is not the answer
  and is not needed.

## Editor configuration

The generated `tsconfig.json` needs no editor setup — every editor already reads
it. The rest of this section is for the optional hook.

### VS Code

Add to `.vscode/settings.json`:

```json
{
  "typescript.tsserver.nodeOptions": "--require .bazel/tsserver-hook.js"
}
```

Restart the TS server: `Cmd+Shift+P` → `TypeScript: Restart TS Server`.

### Neovim (coc-tsserver)

Add to `coc-settings.json`:

```json
{
  "tsserver.tsserver.nodeOptions": "--require .bazel/tsserver-hook.js"
}
```

### Neovim (nvim-lspconfig + typescript-language-server)

```lua
require('lspconfig').ts_ls.setup({
  init_options = {
    tsserver = {
      nodeOptions = "--require .bazel/tsserver-hook.js",
    },
  },
})
```

### Emacs (lsp-mode)

```elisp
(setq lsp-clients-typescript-server-args
  '("--stdio" "--tsserver-path" "tsserver"
    "--tsserver-log-verbosity" "off"
    "--tsserver-nodeOptions" "--require .bazel/tsserver-hook.js"))
```

### Any editor with tsserver

The hook works with any editor that runs tsserver through Node.js. Pass `--require .bazel/tsserver-hook.js` as a Node flag when starting tsserver.

## How It Works

The hook is TypeScript's equivalent of Go's
[GOPACKAGESDRIVER](https://jayconrod.com/posts/125/go-editor-support-in-bazel-workspaces),
with one deliberate difference: **it never runs Bazel**. Everything Bazel knows
arrives through `.bazel/tsserver-hook-data.json`, which `refresh_tsconfig` wrote
at analysis time. A long-lived editor process asking the Bazel server for
anything would sit on the same lock a build wants.

1. **Worker thread** reads `.bazel/tsserver-hook-data.json` — the npm entry
   points, the `ts_compile` package list, the `module_name` specifiers, the path
   aliases — and turns it into a module-name → declaration-path map
2. **npm packages** resolved from the declarations installed under `npm_dir`,
   the same set the generated `tsconfig.json` names
3. **Internal packages** resolved from `bazel-bin` (`.d.ts` after a build) or the
   source tree (`.ts` before one)
4. **Fragments**, if the `.bazelrc` lines above are in place, add the packages and
   aliases of every target the aspect reached, including the ones no rule may
   name. One target built in two configurations writes two fragments; they are
   deduplicated by label, first config root in sorted order winning, so the merge
   does not depend on what `bazel-out` happens to hold
5. **Path aliases** come from that graph data, plus a scan of
   `# gazelle:ts_path_alias` directives in BUILD files to cover directives added
   since the last refresh — the graph wins, since it is what the build actually
   resolves
6. **File watcher** watches the graph data file, the root `BUILD.bazel` and
   `pnpm-lock.yaml`, and `bazel-bin` recursively for new `.d.ts` and new
   fragments; a change to any of them rebuilds the map

The main thread is never blocked — the worker builds the map off-thread and posts
it back. tsserver returns "unresolved" briefly on first load, then resolves once
the worker completes.

### Resolution priority

1. `.d.ts` in `bazel-bin` — fast, precise (available after `bazel build`)
2. `.ts` source file — always available, slower for tsserver to process
3. npm declarations under `npm_dir` — whatever the last
   `bazel run //:refresh_tsconfig` installed

### What a build buys

First-party resolution works without `bazel build`: the source `.ts` files are
always on disk. `bazel build` improves it by providing `.d.ts` files — and, with
the aspect enabled, by writing the fragments that name the packages `deps` could
not reach.

npm resolution is bounded by the refresh rather than by the build. The packages
that resolve are the ones reachable from `deps` when `refresh_tsconfig` last ran,
in both the `paths` entries and the hook — so an import that pulls in a package
none of those targets reached is unknown until you re-run the target. That is the
same re-run the staleness test asks for, which is the point of turning it on.

## Debugging

Set `TSSERVER_HOOK_DEBUG=1` in your environment to see resolution decisions in the tsserver log.

## Debugging Tests in VS Code

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
