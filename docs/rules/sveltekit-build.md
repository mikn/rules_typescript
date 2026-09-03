# sveltekit_build

Wraps a `vite build` driven by SvelteKit's own Vite plugin as a single Bazel
action, and returns both halves of a SvelteKit application: the browser bundle
under `client/` and the request handler under `server/`.

The action stages a SvelteKit project directory from declared inputs alone: the
`src/` tree, a `node_modules` tree, `svelte.config.js`, the Vite config carrying
the plugin. It makes that directory the process working directory, runs the
build in it, and moves `.svelte-kit/output` into one declared output artifact.
SvelteKit reads only the working directory; see
[The Working Directory](#the-working-directory).

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
The two configs are hand-authored and carry no Bazel-specific lines:

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

Pin `kit.version.name`. See [Reproducibility](#reproducibility).

### Gazelle-Generated `srcs`

Gazelle writes `srcs` as a `glob()` over `src/` plus the assets tree, and
recomputes it on every run. The assets tree is `kit.files.assets`, which
defaults to `static/` but relocates; Gazelle reads the name out of
`svelte.config.js` and reports the forms it cannot read:

```
typescript: SvelteKit detected: svelte.config.js sets kit.files.assets in a form
Gazelle cannot read, so srcs globs static/ and the assets tree may be missing
from it -- an app whose assets are not staged builds green and 404s on them. Name
the tree in srcs with a "# keep" comment on its pattern.
```

`staging_srcs` is generated from every target outside `src/` and the assets
tree.

A pattern or a label Gazelle does not derive needs a `# keep` on its line, and
so do hand-set `config` and `svelte_config` values. Full contract:
[Attributes Gazelle owns on the framework rules](../gazelle/directives.md#attributes-gazelle-owns).

## Attributes

| Attribute | Type | Default | Description |
|-----------|------|---------|-------------|
| `srcs` | `label_list` | required | Every project file SvelteKit reads: `src/app.html`, the route tree under `src/routes/`, `src/lib/**`, and anything they import from inside this package |
| `svelte_config` | `label` | required | `svelte.config.js`. That extension only |
| `config` | `label` | required | The Vite config whose default export carries SvelteKit's plugin |
| `node_modules` | `label` | required | A [`node_modules`](node-modules.md) target holding `@sveltejs/kit`, `@sveltejs/vite-plugin-svelte`, `svelte`, `vite` and the app's dependencies |
| `config_srcs` | `label_list` | `[]` | Local modules `config` imports, staged beside it |
| `staging_srcs` | `label_list` | `[]` | Files from other packages, staged at their workspace-relative paths |
| `tsconfig` | `label` | `None` | A `tsconfig.json` staged at the project root |
| `env` | `string_dict` | `{}` | Extra environment variables for the build |
| `allow_subpackages` | `list` | `[]` | Directories under the globbed tree that are Bazel packages of their own, accepted as holes in `srcs` |
| `allow_network` | `bool` | `False` | Let the build reach the network. The action otherwise runs with `block-network` |

Each `srcs` file lands at its path relative to the target's package.
`src/app.html` and the route tree are read off disk, not through imports, and
must be listed. `svelte_config` takes a `.js` file only; see
[Checks the Rule Performs](#checks-the-rule-performs). The rule stages `config`
in a subdirectory of the project root, so its relative imports resolve against
`config_srcs` and not against the app sources. `staging_srcs` has the same
contract as [`next_build`](next-build.md)'s. `allow_subpackages` is described
under
[Subpackages under the globbed tree](#subpackages-under-the-globbed-tree).

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

No adapter is run: the output is SvelteKit's own build, not a platform-specific
deployment bundle.

## The Working Directory

SvelteKit reads `process.cwd()` and nothing else. `load_config()` globs
`<cwd>/svelte.config.{js,ts}`, then resolves every `kit.files.*` entry
(`src/app.html`, `src/routes`, `src/lib`, `static`) against that directory, from
Vite's `config` hook, before any module is transformed. It scans the route tree
off disk and writes `<cwd>/.svelte-kit`.

The plugin's `config` hook returns `root: cwd` and warns when the host config set
`root` to something else. An app staged anywhere but the working directory fails
with `src/app.html does not exist` whatever `root` says.
[`ts_bundle`](ts-bundle.md)'s Vite wrapper redirects `root` to its staging
directory and does not `cd` there, so it cannot host this build. The plugin also
replaces `build.outDir`, `build.rollupOptions.input`, the output filename
patterns, `base`, `publicDir`, `build.manifest` and `build.ssr` with its own
values, and the bundle lands under `.svelte-kit/` in the working directory
whatever `build.outDir` the host config set.

One `vite build` is two builds: `build.ssr` starts true, and the SSR half's
`writeBundle` flips it and calls `vite.build()` again for the client. Neither
half is independently cacheable, so both come out of one action.

## Reproducibility

`kit.version.name` defaults to `Date.now().toString()`, evaluated when
SvelteKit's options module loads. It lands in `client/_app/version.json` and is
hashed into the `__sveltekit_<hash>` global, so it changes chunk content and
chunk filenames. Unpinned, no two builds agree.

The rule checks the emitted `version.json` after the build and warns when the
version reads as epoch-milliseconds. A pinned config:

```javascript
export default { kit: { version: { name: "1" } } };
```

`//tests/integration:sveltekit_test` builds twice from clean and compares the
full set of hashed client filenames.

## Subpackages Under the Globbed Tree

`glob()` does not descend into a subpackage. A BUILD file under the globbed tree
removes its directory from `srcs`, and SvelteKit compiles the route tree minus
that directory and reports success. Without the check below, a BUILD file under
`src/routes/blog/[slug]` leaves `server/manifest.js` with the other routes only:

```console
$ grep 'id:' bazel-bin/app_sveltekit_out/server/manifest.js
    id: "/",
    id: "/api",
```

`sveltekit_build` is a macro. Before instantiating the rule it asks
`native.subpackages()` which directories under the globbed tree are packages of
their own, and fails naming the first:

```
sveltekit_build(name = "app"): src/routes/blog/[slug]/BUILD.bazel makes
src/routes/blog/[slug] a Bazel package, and the srcs glob does not descend into
one.
```

Delete the BUILD file. Gazelle does not create one under `src/` and reports any
it finds there. It can only empty a file it did not write, and an empty BUILD
file is still a package, so the file has to be deleted. A deliberate subpackage
whose contents reach the app through `staging_srcs` is listed:

```python
allow_subpackages = ["src/routes/blog/[slug]"],
```

## Checks the Rule Performs

Three failure modes SvelteKit itself does not report. The rule fails on each:

- **A route file SvelteKit never compiled.** The keys of
  `server/.vite/manifest.json` are the staged paths verbatim, so the rule
  requires every staged `+`-prefixed file to appear there and lists the ones
  that do not. A route tree staged outside `kit.files.routes` is the usual
  cause.
- **A build that staged no routes at all.** SvelteKit emits
  `entries/fallbacks/` either way and the summary looks normal, so the rule
  fails at analysis time when no `+page*` or `+server*` file is in `srcs`.
- **A `svelte.config.mjs`.** The rule stages the config as `svelte.config.js`
  whatever it is called, so a `.mjs` builds here and nowhere else: SvelteKit's
  `load_config()` globs `svelte.config.{js,ts}` at its cwd. Rejected at analysis
  time. A `.ts` is in that glob, but `load_config()` imports it through Node,
  and the toolchain Node cannot load a `.ts` file (`ERR_UNKNOWN_FILE_EXTENSION`).
  Rejected too.

`//tests/integration:sveltekit_test` covers all of these, the subpackage hole
above, a missing `src/app.html` and the unpinned-version warning.

## Hermeticity

The action runs with `block-network`. SvelteKit itself has no telemetry and
fetches nothing. A `config` plugin is arbitrary code; a Google-Fonts or
remote-schema plugin needs the network. `allow_network = True` opts a target
out.

## No TypeScript Targets Under `src/`

Gazelle generates no `ts_compile` or `ts_test` anywhere under a SvelteKit app's
`src/`, and prints that once per run. Two reasons:

- A BUILD file under `src/` makes a subpackage, and `glob()` does not descend
  into one. See
  [Subpackages Under the Globbed Tree](#subpackages-under-the-globbed-tree).
- A route module conventionally imports `./$types`, which exists only as
  `.svelte-kit/types/**/$types.d.ts` and is reachable only through the
  `rootDirs` scheme in the `tsconfig.json` SvelteKit generates. A plain
  `ts_compile` has neither.

TypeScript compiled and tested by Bazel goes in a package outside `src/`;
Gazelle names it in `staging_srcs`.

## Not Covered Yet

- **Adapters.** The build stops at `.svelte-kit/output`. An adapter runs in
  `closeBundle` and conventionally writes to `build/` at the project root, which
  the rule does not relocate.
- **Type-checking `<script lang="ts">`.** That needs `svelte-check`, which pulls
  `svelte2tsx` and `typescript`. It consumes the generated
  `.svelte-kit/tsconfig.json` and `$types`, so it needs a `svelte-kit sync`
  output as an input, or must run sync itself.
  [`svelte_library`](svelte-library.md) has the same gap.
- **A dev server.** SvelteKit's dev mode writes `.svelte-kit` continuously and
  needs a writable project root outside the source tree. `ts_dev_server`'s
  `DevServerInfo` is the seam for it.
