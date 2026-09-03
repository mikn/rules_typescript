# next_build

Wraps `next build` as a single Bazel action. The action stages a Next.js project
directory from declared inputs alone: the sources, a `node_modules` tree, the
config, an optional `tsconfig.json`. It runs `next build` in that directory with
the network blocked and returns the `.next/` directory as one output artifact.

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
| `srcs` | `label_list` | required | Every project file Next.js reads: routes, components, stylesheets, `middleware.ts`, `public/` assets |
| `node_modules` | `label` | required | A [`node_modules`](node-modules.md) target holding `next`, `react`, `react-dom`, `typescript`, the `@types` packages and the app's dependencies |
| `staging_srcs` | `label_list` | `[]` | Files from other targets, staged at their workspace-relative paths |
| `config` | `label` | `None` | The `next.config.{mjs,js,ts}` file |
| `config_srcs` | `label_list` | `[]` | Modules `config` imports, staged beside it |
| `tsconfig` | `label` | `None` | A `tsconfig.json` staged into the project root |
| `env` | `string_dict` | `{}` | Extra environment variables for the build |
| `allow_network` | `bool` | `False` | Let `next build` reach the network. See [Hermeticity](#hermeticity) |

Each `srcs` file lands at its path relative to the target's package; a file from
another package has no path inside the project root and fails with an error
naming `staging_srcs`. `next build` npm-installs `typescript` and the `@types`
packages when they are missing; the action has no network, so the `node_modules`
tree has to carry them. Without `config`, Next.js uses its defaults; without
`tsconfig`, it writes its own default, with no `paths` aliases. `config` is a
single file, so a config with a sibling import fails with `ERR_MODULE_NOT_FOUND`
until that sibling is listed in `config_srcs`. `NEXT_TELEMETRY_DISABLED` and
`NEXT_PRIVATE_SKIP_PATCHING` are always set.

## Files `srcs` Must List

The staging directory contains the declared inputs and nothing else; a missing
file surfaces as a module-resolution failure. Three are easy to miss:

- **Stylesheets.** `import "./globals.css"` is
  `Module not found: Can't resolve './globals.css'` until `**/*.css` is in
  `srcs`.
- **`public/`.** A static `import logo from "../../public/logo.png"` is
  `Module not found` until `public/**` is in `srcs`. Nothing else reads
  `public/` at build time; see [Serving the Output](#serving-the-output).
- **`middleware.ts`.** It sits beside `app/`, not under it, so an `app/**` glob
  misses it. Nothing imports it, so the omission fails no import. The compiled
  middleware under `server/` and the matcher in `server/middleware-manifest.json`
  show whether it was staged.

App Router pages and layouts, `route.ts` handlers, Pages Router pages with
`getServerSideProps`, `pages/api/*`, and a `"use client"` component calling a
`"use server"` action all compile with no extra attrs.

### Gazelle-Generated `srcs`

Gazelle writes `srcs` as a `glob()` over the directories Next.js owns and
recomputes it on every run. The glob covers the three files above. A pattern
Gazelle does not derive is dropped on the next run unless a `# keep` holds it;
the same applies to `staging_srcs`, `config` and `tsconfig`.

```python
srcs = glob([
    "app/**",
    "content/**",  # keep
]),
```

The run names every value it drops. Full contract:
[Attributes Gazelle owns on the framework rules](../gazelle/directives.md#attributes-gazelle-owns).

## `staging_srcs`

`staging_srcs` takes any target's files. A shared package has two shapes:

```python
staging_srcs = [
    "//packages/shared/src:sources",  # a filegroup of .ts — Next.js compiles them
    "//packages/shared/src",          # a ts_compile target — Next.js bundles its .js
]
```

In the second, `ts_compile` stages `index.js` beside the `index.d.ts` that types
it; Next.js bundles the compiled JavaScript and type-checks the import through
the declaration. Bazel caches the compile.

Either way the files land at their workspace-relative paths, so a relative
import (`../../packages/shared/src/index`) resolves with no `transpilePackages`
rewriting.

Gazelle generates `staging_srcs` from the packages it finds outside the owned
directories. A label it cannot derive, such as a `filegroup` over a vendored
tree, needs a `# keep` on its line:

```python
staging_srcs = [
    "//packages/shared/src:sources",
    "//vendor:legacy",  # keep
]
```

## Hermeticity

The action runs with `block-network`. The sandbox enforces it: only a sandbox
that creates its own network namespace blocks the network. `--spawn_strategy=local`
has no sandbox. `processwrapper-sandbox`, Bazel's fallback where unprivileged
user namespaces are unavailable (including a Bazel nested inside another Bazel's
sandbox), ignores the requirement. The requirement is on the action either way;
`bazel aquery 'mnemonic("NextBuild", //:your_target)'` shows it.

`next/font/google` is the one common feature this rejects: it downloads the font
CSS and the woff2 payloads while compiling. `next/font/local` is unaffected: the
font file is an input and lands in `static/media/`.

A failed build whose log matches a network error gets a message naming the
cause:

```
next_build: `next build` failed while reaching for the network.
...
Either declare the font locally -- `next/font/local`, with the font files listed
in `srcs` -- or set `allow_network = True` on this next_build target to accept a
build whose output depends on the network.
```

The wrapper prints it when a failed build's output matches `ENETUNREACH`,
`EAI_AGAIN`, `ECONNREFUSED`, `getaddrinfo` or `from Google Fonts`. A failure
matching none of them exits with Next.js's own message alone.

`allow_network = True` swaps `block-network` for `requires-network`. The
target's output then depends on a remote host.

### Reproducibility

Two builds of identical inputs do not produce identical bytes:

- Next.js bakes the absolute project path into its server bundles. Under
  sandboxing that path includes the sandbox run number.
- `BUILD_ID` is a random nanoid unless `next.config` sets `generateBuildId`.
- The server-actions encryption key is a fresh AES-GCM key per build unless
  `NEXT_SERVER_ACTIONS_ENCRYPTION_KEY` is set, which the `env` attr can do.
  Two builds with different keys cannot decrypt each other's action arguments.

## Output

One directory artifact, `<name>_next_out`, holding the `.next/` tree: `server/`
(route bundles, prerendered HTML, the route and middleware manifests), `static/`
(client chunks, `css/`, `media/`), `BUILD_ID`, and the top-level manifests
`next start` reads.

`next start` reads the tree by name and the file set depends on which routes
were prerendered, so the rule removes a fixed list and keeps the rest:

| Removed | Contents |
|---------|----------|
| `cache/` | The local incremental cache; machine-specific and large |
| `trace` | Every build span, tagged with the absolute staging path and a timestamp |
| `diagnostics/` | Build timings |

Nothing serves from any of them. Everything else `next build` writes stays.

Where each convention lands:

| Convention | Output |
|------------|--------|
| App Router page | `server/app/<route>/page.js`, plus `server/app/<route>.html` and its `.rsc`/`.meta` siblings when prerendered |
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

### Serving the Output

`next build` does not copy `public/` into `.next`; Next.js serves it from the
project directory at request time. Serving also needs the config, a
`package.json` and the npm tree beside the output. What runs the artifact is
outside this rule.

## Type Checking

`next build` runs TypeScript itself, over the staged `tsconfig.json`. A type
error in any staged file fails the Bazel action, including a file that arrived
through `staging_srcs`. There is no separate validation action and no `deps`
attr.
