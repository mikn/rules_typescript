# sveltekit_build

Wraps a `vite build` driven by SvelteKit's own Vite plugin as a single Bazel
action, and returns **both** halves of a SvelteKit application: the browser
bundle under `client/` and the request handler under `server/`.

The action stages a SvelteKit project directory from declared inputs alone — the
`src/` tree, a `node_modules` tree, `svelte.config.js`, the Vite config carrying
the plugin — makes that directory the process working directory, runs the build
in it, and moves `.svelte-kit/output` into one declared output artifact.

The working directory is the whole point: it is the only thing SvelteKit looks
at. See [Why this is a rule and not a `ts_bundle`](#why-this-is-a-rule-and-not-a-ts_bundle).

## Usage

```python
load("@rules_typescript//npm:defs.bzl", "node_modules")
load("@rules_typescript//ts:defs.bzl", "sveltekit_build")

node_modules(
    name = "node_modules",
    deps = [
        "@npm//:svelte",
        "@npm//:sveltejs_kit",
        "@npm//:sveltejs_vite-plugin-svelte",
        "@npm//:vite",
    ],
)

sveltekit_build(
    name = "app",
    srcs = glob(["src/**"]),
    config = "vite.config.mjs",
    node_modules = ":node_modules",
    svelte_config = "svelte.config.js",
)
```

Gazelle generates both targets when it sees `@sveltejs/kit` in `package.json`.
You hand-author the two configs, which stay plain and portable — nothing in
either is Bazel-specific:

```javascript
// vite.config.mjs
import { sveltekit } from "@sveltejs/kit/vite";

export default { plugins: [await sveltekit()] };
```

```javascript
// svelte.config.js
export default {
  kit: {
    version: { name: "1" },
  },
};
```

Pin `kit.version.name`. See [Reproducibility](#reproducibility-pin-kitversionname).

### Under Gazelle, `srcs` is generated

Gazelle writes `srcs` as a `glob()` over `src/` plus the assets tree, and
recomputes it on every run. The assets tree is `kit.files.assets`, which
defaults to `static/` but relocates — Gazelle reads the name out of
`svelte.config.js`, and says so when the option is set in a form it cannot read:

```
typescript: SvelteKit detected: svelte.config.js sets kit.files.assets in a form
Gazelle cannot read, so srcs globs static/ and the assets tree may be missing
from it -- an app whose assets are not staged builds green and 404s on them. Name
the tree in srcs with a "# keep" comment on its pattern.
```

`staging_srcs` is generated the same way, from the other side of the glob:
Gazelle names every target it writes outside `src/` and the assets tree, so a
shared package the glob cannot cover still reaches the build.

Because Gazelle recomputes both attributes, a pattern or a label it does not
derive needs a `# keep` on its line, and so do hand-set `config` and
`svelte_config` values. Full contract:
[Attributes Gazelle owns on the framework rules](../gazelle/directives.md#attributes-gazelle-owns).

## Attributes

| Attribute | Type | Default | Description |
|-----------|------|---------|-------------|
| `srcs` | `label_list` | required | Every project file SvelteKit reads: `src/app.html`, the route tree under `src/routes/`, `src/lib/**`, and anything they import from inside this package. Each lands at its path relative to the target's package. `src/app.html` and the route tree are read **off disk**, not through imports, so they must be listed even though nothing imports them |
| `svelte_config` | `label` | required | `svelte.config.js`. That extension only — see [Silent failures the rule turns into loud ones](#silent-failures-the-rule-turns-into-loud-ones) |
| `config` | `label` | required | The Vite config whose default export carries SvelteKit's plugin. The rule stages it in a subdirectory of the project root, so its relative imports resolve against `config_srcs` rather than against the app sources |
| `node_modules` | `label` | required | A [`node_modules`](node-modules.md) target holding `@sveltejs/kit`, `@sveltejs/vite-plugin-svelte`, `svelte`, `vite` and the app's dependencies |
| `config_srcs` | `label_list` | `[]` | Local modules `config` imports, staged beside it |
| `staging_srcs` | `label_list` | `[]` | Files from other packages, staged at their workspace-relative paths. Same contract as [`next_build`](next-build.md)'s `staging_srcs` |
| `tsconfig` | `label` | `None` | A `tsconfig.json` staged at the project root |
| `env` | `string_dict` | `{}` | Extra environment variables for the build |
| `allow_subpackages` | `list` | `[]` | Directories under the globbed tree that are Bazel packages of their own, accepted as holes in `srcs`. See [A BUILD file in the tree is a hole in the app](#a-build-file-in-the-tree-is-a-hole-in-the-app) |
| `allow_network` | `bool` | `False` | Let the build reach the network. The action otherwise runs with `block-network` |

## Output

One directory artifact, `<name>_sveltekit_out`, holding what SvelteKit wrote to
`.svelte-kit/output`:

```
client/_app/immutable/entry/{start,app}.<hash>.js
client/_app/immutable/nodes/<n>.<hash>.js        one per route node
client/_app/immutable/{chunks,assets}/**
client/_app/version.json                         kit.version.name
client/.vite/manifest.json
server/index.js                                  the request handler
server/manifest.js                               route ids, patterns, nodes
server/entries/pages/_page.svelte.js             one per route file
server/entries/pages/_page.server.ts.js
server/entries/endpoints/**/_server.ts.js        one per +server route
server/entries/fallbacks/{layout,error}.svelte.js
server/chunks/**
server/.vite/manifest.json
```

Route entries are named after the route files, with `+` rewritten to `_`
because `+` is not legal in an output filename.

No adapter is run: this is SvelteKit's own build, not a platform-specific
deployment bundle. An adapter would additionally write its own tree, which this
rule does not yet relocate.

## Why this is a rule and not a `ts_bundle`

SvelteKit reads `process.cwd()`, and only `process.cwd()`. `load_config()` globs
`<cwd>/svelte.config.{js,ts}`, then resolves every `kit.files.*` entry —
`src/app.html`, `src/routes`, `src/lib`, `static` — against that same directory.
It does the work from Vite's `config` hook, before a single module is
transformed: it scans the route tree off disk and writes `<cwd>/.svelte-kit`.

Nothing redirects that. The plugin's own `config` hook returns `root: cwd` and
only *warns* when the host config set `root` to something else, so
[`ts_bundle`](ts-bundle.md)'s staging-root redirection is inert here: an app
staged anywhere but the working directory fails with `src/app.html does not
exist` no matter what `root` says. Hosting SvelteKit inside `ts_bundle` would
mean changing where the shared Vite wrapper `cd`s — for TanStack, Remix and
every future Vite framework — to serve this one.

The plugin also replaces `build.outDir`, `build.rollupOptions.input`, the output
filename patterns, `base`, `publicDir`, `build.manifest` and `build.ssr` with its
own values, so a `ts_bundle` output directory would sit empty while the real
bundle landed in an undeclared `.svelte-kit/`.

One `vite build` is two builds: `build.ssr` starts true, and the SSR half's
`writeBundle` flips it and calls `vite.build()` again for the client. The two
halves come out of one action because neither is independently cacheable.

## Reproducibility: pin `kit.version.name`

`kit.version.name` defaults to `Date.now().toString()`, evaluated when
SvelteKit's options module loads. It lands in `client/_app/version.json` **and**
is hashed into the `__sveltekit_<hash>` global, so it changes chunk *content* and
therefore chunk *filenames*. Unpinned, no two builds agree and nothing
downstream of the target ever gets a cache hit — a green build that quietly
never caches.

The rule cannot see the config's contents at analysis time, so it checks the
emitted `version.json` afterwards and warns when the version reads as
epoch-milliseconds, which is what the default produces. Pin it:

```javascript
export default { kit: { version: { name: "1" } } };
```

`//tests/integration:sveltekit_test` builds twice from clean and byte-compares
the full set of hashed client filenames.

## A BUILD file in the tree is a hole in the app

`glob()` does not descend into a subpackage. A BUILD file anywhere under the
globbed tree therefore removes its whole directory from `srcs`, SvelteKit
compiles the route tree minus that directory, and the build reports success for
the routes that survived — a deployed app one route short, with nothing in the
log:

```console
$ touch 'src/routes/blog/[slug]/BUILD.bazel'
$ bazel build //:app
INFO: Build completed successfully
$ grep 'id:' bazel-bin/app_sveltekit_out/server/manifest.js
    id: "/",
    id: "/api",
```

`sveltekit_build` is a macro for that reason. Before the rule is instantiated it
asks `native.subpackages()` which directories under the globbed tree are packages
of their own, and fails naming each one:

```
sveltekit_build(name = "app"): src/routes/blog/[slug]/BUILD.bazel makes
src/routes/blog/[slug] a Bazel package, and the srcs glob does not descend into
one.
```

Delete the BUILD file. Gazelle will not create one under `src/` and names any it
finds there, but *emptying* a file — all it can do to one it did not write —
leaves the package behind, so the file itself has to go. If a subpackage is
deliberate and its contents reach the app through `staging_srcs`, list it:

```python
allow_subpackages = ["src/routes/blog/[slug]"],
```

## Silent failures the rule turns into loud ones

SvelteKit degrades instead of failing, and each case produces a green Bazel
target with the framework barely involved:

- **A route file SvelteKit never compiled.** Only SvelteKit knows which of the
  staged files it treated as routes, and it writes that down: the keys of
  `server/.vite/manifest.json` are the staged paths verbatim. The rule requires
  every staged `+`-prefixed file to appear there, and names the ones that do
  not. A route tree staged outside `kit.files.routes` is the usual cause.
- **A build that staged no routes at all.** SvelteKit emits
  `entries/fallbacks/` either way and the summary looks normal, so the rule
  fails at analysis time when no `+page*` or `+server*` file is in `srcs`.
- **A `svelte.config.mjs` that only Bazel would read.** The rule stages the
  config as `svelte.config.js` whatever it is called, so a `.mjs` builds here
  and nowhere else — SvelteKit's own `load_config()` globs
  `svelte.config.{js,ts}` at its cwd and would not find it. Rejected at
  analysis time. A `.ts` is rejected too, for a different reason: it is in that
  glob, but `load_config()` imports what it finds through Node, and the
  toolchain Node cannot load a `.ts` file at all
  (`ERR_UNKNOWN_FILE_EXTENSION`).

All of these are pinned in `//tests/integration:sveltekit_test`, alongside the
subpackage hole above, a missing `src/app.html` and the unpinned-version
warning.

## Hermeticity

The action runs with `block-network`. SvelteKit itself needs no network — it has
no telemetry and fetches nothing — but a `config` plugin is arbitrary code, and
a Google-Fonts or remote-schema plugin would otherwise make the output depend on
a host. `allow_network = True` opts a target out and says so in the BUILD file.

## `src/` gets no TypeScript targets

Gazelle generates no `ts_compile` or `ts_test` anywhere under a SvelteKit app's
`src/`, and says so once per run. Two reasons, and the first is decisive:

- A BUILD file under `src/` makes a subpackage, and `glob()` does not descend
  into a subpackage — so the staged tree would silently lose exactly the modules
  the app imports. See [A BUILD file in the tree is a
  hole](#a-build-file-in-the-tree-is-a-hole-in-the-app) for what happens when
  one is already there.
- A route module conventionally imports `./$types`, which exists only as
  `.svelte-kit/types/**/$types.d.ts` and is reachable only through the
  `rootDirs` scheme in the `tsconfig.json` SvelteKit generates. A plain
  `ts_compile` has neither.

TypeScript you want compiled and unit-tested by Bazel goes in a package outside
`src/`, which Gazelle names in `staging_srcs`.

## Not covered yet

- **Adapters.** The build stops at `.svelte-kit/output`. An adapter runs in
  `closeBundle` and conventionally writes to `build/` at the project root, which
  the rule does not relocate.
- **Type-checking `<script lang="ts">`.** That needs `svelte-check`, which pulls
  `svelte2tsx` and `typescript`, and it consumes the generated
  `.svelte-kit/tsconfig.json` and `$types` — so it needs a `svelte-kit sync`
  output as an input, or to run sync itself. [`svelte_library`](svelte-library.md)
  has the same gap for the same reason.
- **A dev server.** `ts_dev_server`'s `DevServerInfo` seam is the right place,
  but SvelteKit's dev mode writes `.svelte-kit` continuously and would need a
  writable project root outside the source tree.
