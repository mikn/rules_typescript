# next_build

Wraps `next build` as a single Bazel action. Next.js owns the compilation; the
rule owns what the compiler is allowed to see and what comes back out.

The action stages a Next.js project directory from declared inputs alone — the
sources, a `node_modules` tree, the config, an optional `tsconfig.json` — runs
`next build` in it with the network blocked, and returns the `.next/` directory
as one output artifact.

## Usage

```python
load("@rules_typescript//npm:defs.bzl", "node_modules")
load("@rules_typescript//ts:defs.bzl", "next_build")

node_modules(
    name = "node_modules",
    deps = [
        "@npm//:next",
        "@npm//:react",
        "@npm//:react-dom",
        "@npm//:types_node",
        "@npm//:types_react",
        "@npm//:typescript",
    ],
)

next_build(
    name = "app",
    srcs = glob([
        "src/app/**/*.tsx",
        "src/app/**/*.ts",
        "src/app/**/*.css",
        "src/pages/**/*.tsx",
        "src/pages/**/*.ts",
        "src/middleware.ts",
        "public/**",
    ]),
    config = "next.config.mjs",
    node_modules = ":node_modules",
    tsconfig = "tsconfig.json",
)
```

## Attributes

| Attribute | Type | Default | Description |
|-----------|------|---------|-------------|
| `srcs` | `label_list` | required | Every project file Next.js reads: routes, components, stylesheets, `middleware.ts`, `public/` assets. Each lands at its path relative to the target's package, so a file from another package is an error naming `staging_srcs` |
| `node_modules` | `label` | required | A [`node_modules`](node-modules.md) target holding `next`, `react`, `react-dom` and the app's dependencies — plus `typescript` and the `@types` packages, which `next build` otherwise npm-installs itself, with no network to do it |
| `staging_srcs` | `label_list` | `[]` | Files from other targets, staged at their workspace-relative paths. Both a `filegroup` of `.ts` sources and a `ts_compile` target work — see below |
| `config` | `label` | `None` | The `next.config.{mjs,js,ts}` file. Without one Next.js uses its defaults |
| `config_srcs` | `label_list` | `[]` | Modules `config` imports. `config` is a single file, so a config with a sibling import fails with `ERR_MODULE_NOT_FOUND` until that sibling is listed here |
| `tsconfig` | `label` | `None` | A `tsconfig.json` staged into the project root. Without it Next.js writes its own default, and any `paths` aliases the app relies on are gone |
| `env` | `string_dict` | `{}` | Extra environment variables for the build. `NEXT_TELEMETRY_DISABLED` and `NEXT_PRIVATE_SKIP_PATCHING` are always set |
| `allow_network` | `bool` | `False` | Let `next build` reach the network. See [Hermeticity](#hermeticity) |

## What `srcs` has to cover

The staging directory contains nothing but the declared inputs, which is what
makes `srcs` a real declaration — and also means an omission shows up as a
module-resolution failure rather than as a file quietly read off the developer's
disk. Three that are easy to miss:

- **Stylesheets.** `import "./globals.css"` is
  `Module not found: Can't resolve './globals.css'` until `**/*.css` is in
  `srcs`.
- **`public/`.** A static `import logo from "../../public/logo.png"` is
  `Module not found` until `public/**` is in `srcs`. (Nothing else needs
  `public/` at build time — see
  [The output is not a deployment unit](#the-output-is-not-a-deployment-unit).)
- **`middleware.ts`.** It sits beside `app/`, not under it, so an `app/**` glob
  misses it — and this one does not fail. The build stays green with an empty
  `middleware-manifest.json`, so the middleware silently stops existing. Worth
  an assertion on the output rather than trust.

Everything else is ordinary: App Router pages and layouts, `route.ts` handlers,
Pages Router pages with `getServerSideProps`, `pages/api/*`, and a `"use client"`
component calling a `"use server"` action all compile with no extra attrs.

### Under Gazelle, `srcs` is generated

The Gazelle extension writes `srcs` as a `glob()` over the directories Next.js
owns and recomputes it on every run, so it covers the three above without being
told. It also **owns the attribute**: a pattern you add that Gazelle does not
derive is dropped on the next run unless a `# keep` holds it, and the same goes
for `staging_srcs`, `config` and `tsconfig`.

```python
srcs = glob([
    "app/**",
    "content/**",  # keep
]),
```

The run names every value it drops. Full contract:
[Attributes Gazelle owns on the framework rules](../gazelle/directives.md#attributes-gazelle-owns).

## staging_srcs: sources or compiled output

`staging_srcs` takes any target's files, which gives two shapes for a shared
package:

```python
staging_srcs = [
    "//packages/shared/src:sources",  # a filegroup of .ts — Next.js compiles them
    "//packages/shared/src",          # a ts_compile target — Next.js bundles its .js
]
```

The second is the hybrid boundary: `ts_compile` stages `index.js` beside the
`index.d.ts` that types it, so Next.js bundles already-compiled JavaScript and
type-checks the import through the declaration. Shared code compiles once and is
cached by Bazel instead of being recompiled by every app that imports it.

Either way the files land at their workspace-relative paths, so a relative
import (`../../packages/shared/src/index`) resolves with no `transpilePackages`
rewriting.

Under Gazelle, `staging_srcs` is generated from the packages it finds outside the
owned directories — so a label it cannot derive, like a `filegroup` you wrote over
a vendored tree, needs a `# keep` on its line:

```python
staging_srcs = [
    "//packages/shared/src:sources",
    "//vendor:legacy",  # keep
]
```

## Hermeticity

The action runs with `block-network`. `next build` reaches for the network on its
own initiative, so the sandbox takes the option away rather than the rule
trusting an environment variable to have covered every path. The enforcement is
the sandbox's, and only a sandbox that can create a network namespace of its own
provides it: `--spawn_strategy=local` has no sandbox and so no boundary, and
`processwrapper-sandbox` — what Bazel falls back to where unprivileged user
namespaces are unavailable, including a Bazel nested inside another Bazel's
sandbox — honours the requirement by ignoring it. The requirement is still on the
action, which `bazel aquery 'mnemonic("NextBuild", //:your_target)'` will show;
whether it bites depends on how the build is run.

`next/font/google` is the one common feature this rejects: it downloads the font
CSS and the woff2 payloads while compiling. `next/font/local` is unaffected — the
font file is an input like any other, and lands in `static/media/` — so the
choice is between declaring the font and accepting a download.

The build fails, and the wrapper names the cause instead of leaving an
`ENETUNREACH` from inside a webpack loader to be interpreted:

```
next_build: `next build` failed while reaching for the network.
...
Either declare the font locally -- `next/font/local`, with the font files listed
in `srcs` -- or set `allow_network = True` on this next_build target to accept a
build whose output depends on the network.
```

An app that fetches at build time — `generateStaticParams` or a prerendered page
calling an API — hits the same wall, and the same diagnostic.

`allow_network = True` swaps `block-network` for `requires-network`. It is the
honest form of the trade: the target's output now depends on a remote host, and
the BUILD file says so.

### The output is not byte-reproducible

Two builds of identical inputs do not produce identical bytes, and this is not
cheaply fixable:

- Next.js bakes the absolute project path into its server bundles — under
  sandboxing that path includes the sandbox run number. Not fixable from here.
- `BUILD_ID` is a random nanoid. Next.js takes it from `generateBuildId` in
  `next.config` and falls back to nanoid; no environment variable overrides it,
  so `generateBuildId` is the only way to pin it.
- The server-actions encryption key is a fresh AES-GCM key per build unless
  `NEXT_SERVER_ACTIONS_ENCRYPTION_KEY` is set — which the `env` attr can do.
  Worth pinning for its own sake: two builds otherwise disagree about how to
  decrypt an action's arguments, which a rolling deploy notices.

Assert on behaviour, not on bytes.

## Output

One directory artifact, `<name>_next_out`, holding the `.next/` tree: `server/`
(route bundles, prerendered HTML, the route and middleware manifests), `static/`
(client chunks, `css/`, `media/`), `BUILD_ID`, `types/`, and the top-level
manifests `next start` reads.

A whole-directory output is right here: the consumer is `next start`, which
reads the tree by name, and the file set depends on which routes Next.js decided
to prerender. The pruning is therefore subtractive — these are removed:

| Removed | Why |
|---------|-----|
| `cache/` | A local incremental cache. Machine-specific, and large |
| `trace` | Every build span, tagged with the absolute staging path and a timestamp |
| `diagnostics/` | Build timings |

Nothing serves from any of them.

Where each convention lands, if you need to assert on the output:

| Convention | Output |
|------------|--------|
| App Router page | `server/app/<route>/page.js`, plus `.html`/`.rsc`/`.meta` when prerendered |
| Route handler | `server/app/<route>/route.js` |
| Pages Router page | `server/pages/<route>.js`, or just `<route>.html` when static |
| Pages API route | `server/pages/api/<route>.js` |
| `"use client"` | `server/app/<route>/page_client-reference-manifest.js` |
| `"use server"` | An entry in `server/server-reference-manifest.json` under layer `action-browser` |
| `src/middleware.ts` | `server/src/middleware.js` + `server/edge-runtime-webpack.js`, with the matcher in `server/middleware-manifest.json` |
| Imported CSS | `static/css/<hash>.css` |
| Statically imported image | `static/media/<name>.<hash>.<ext>`, referenced as `/_next/image?url=…` |

Which routes were prerendered and which stayed dynamic is in
`prerender-manifest.json`: a `force-dynamic` route is absent from it.

### The output is not a deployment unit

`next build` does not copy `public/` into `.next` — it is served from the project
directory at request time — and serving also needs the config, a `package.json`
and the npm tree beside the output. The artifact is the build product;
Serving it is a separate concern: the output is a `.next` directory, and what runs it is outside this rule.

## Type checking

`next build` runs TypeScript itself, over the staged `tsconfig.json`. A type
error in any staged file fails the Bazel action, including a file that arrived
through `staging_srcs`. There is no separate validation action and no `deps`
attr: the type check is part of the build.
