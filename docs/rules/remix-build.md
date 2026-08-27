# remix_build

Wraps `remix vite:build` as a single Bazel action and returns **both** halves of
a Remix v2 application: the browser bundle under `client/` and the request
handler under `server/`.

The action stages a Remix project directory from declared inputs alone — the
`app/` sources, a `node_modules` tree, the Vite config carrying the Remix plugin,
an optional `tsconfig.json` — runs the build in it, and moves Remix's build
directory into one declared output artifact.

## Usage

```python
load("@rules_typescript//npm:defs.bzl", "node_modules")
load("@rules_typescript//ts:defs.bzl", "remix_build")

node_modules(
    name = "node_modules",
    deps = [
        "@npm//:react",
        "@npm//:react-dom",
        "@npm//:remix-run_dev",
        "@npm//:remix-run_node",
        "@npm//:remix-run_react",
        "@npm//:vite",
    ],
)

remix_build(
    name = "app",
    srcs = glob([
        "app/**/*.ts",
        "app/**/*.tsx",
    ]),
    config = "remix-vite.config.mjs",
    node_modules = ":node_modules",
)
```

The config is a plain Remix config. Nothing in it is Bazel-specific, because the
rule builds inside a staging root it owns and moves the result out afterwards:

```javascript
import { vitePlugin as remix } from "@remix-run/dev";

export default {
  plugins: [remix({ manifest: true })],
  build: { manifest: true },
};
```

## Attributes

| Attribute | Type | Default | Description |
|-----------|------|---------|-------------|
| `srcs` | `label_list` | required | Every project file Remix reads: `app/root.tsx`, `app/entry.client.tsx`, `app/entry.server.tsx`, `app/routes/**`, and anything they import from inside this package. Each lands at its path relative to the target's package |
| `node_modules` | `label` | required | A [`node_modules`](node-modules.md) target holding `@remix-run/dev` and the app's dependencies |
| `config` | `label` | required | The Vite config whose default export carries the Remix plugin. Remix loads this file itself, so **every** Vite option in it reaches the build. It must not set the plugin's `buildDirectory` |
| `config_srcs` | `label_list` | `[]` | Local modules `config` imports, staged at their paths relative to this package so `./plugins/foo` resolves |
| `staging_srcs` | `label_list` | `[]` | Files from other packages, staged at their workspace-relative paths. Same contract as [`next_build`](next-build.md)'s `staging_srcs` |
| `tsconfig` | `label` | `None` | A `tsconfig.json` staged at the project root |
| `env` | `string_dict` | `{}` | Extra environment variables for the build |

## Output

One directory artifact, `<name>_remix_out`:

```
client/index.html                     the SPA fallback, when the app has one
client/assets/*.js                    route chunks, the client entry, shared chunks
client/assets/manifest-<hash>.js      window.__remixManifest
server/index.js                       the request handler build
.remix/manifest.json                  route id → file / path / parentId  (plugin manifest: true)
.vite/client-manifest.json            (config build.manifest: true)
.vite/server-manifest.json
```

## Why this is a rule and not a `ts_bundle`

`remix vite:build` is not one `vite build`. It loads the Vite config to read the
Remix plugin's own options, then runs `vite build` **twice** against that same
config — once with `build.ssr = false`, once with `build.ssr = true` — and the
plugin's `config` hook replaces `build.outDir` and `build.rollupOptions.input`
differently for each half.

The order is load-bearing. The SSR half's `writeBundle` reads the SSR Vite
manifest and *moves* the SSR-emitted assets into the client directory. The
browser asset manifest — `client/assets/manifest-<hash>.js`, which defines the
`window.__remixManifest` a hydrating client reads — is emitted by the **server**
build and only afterwards belongs to the client. So the two halves are not
independently cacheable, and a build that declares only `client/` as its output
produces a bundle that cannot boot.

[`ts_bundle`](ts-bundle.md)'s Vite wrapper hardcodes exactly one
`vite build --config`, and the config it generates reads only `plugins` and
`root` out of a user `vite_config` — it throws on any other key. A real framework
config exceeds that. Hence a rule whose `config` attr is the config Remix itself
loads.

## SPA mode is a different thing, not a smaller one

A config with `ssr: false` does not produce a smaller version of this output. It
produces something that cannot answer a request:

- a `loader` export becomes a **hard build error**
  (`SPA Mode: N invalid route export(s)`), so no route can load data;
- a resource route compiles to an empty client chunk, because a route that only
  exports a `loader` has nothing to ship to a browser;
- the server directory is deleted after `index.html` is prerendered.

`remix_build` fails, naming the missing half, rather than declaring a partial
output. If you want the client-only bundle, that is the
[`ts_bundle`](ts-bundle.md) path with `vite_config`.

## Testing the server half

The server bundle imports `@remix-run/react`, `react-dom/server` and
`react/jsx-runtime` as bare specifiers and is ESM in a plain `.js`, so running it
needs two things: the `node_modules` tree above it, and a `package.json` saying
`"type": "module"`.

`//tests/integration:remix_ssr_test` does exactly that — it copies `server/` next
to the tree, imports it through `createRequestHandler` from `@remix-run/node`,
and asserts on real responses: loader data in the HTML and in
`window.__remixContext.loaderData`, a nested layout and its child each running
their own loader, a `POST` whose action echoes the submitted form field, and a
resource route answering `text/plain` with no HTML shell.

## Route conventions

Remix derives the route tree from filenames in `app/routes`, one directory level
deep. Gazelle's Remix plugin reads the same conventions so a generated BUILD file
states the tree:

```python
# Remix route routes/_index → / (index, parent root)
# Remix route routes/dash.settings → /dash/settings (parent routes/dash)
# Remix route routes/users.$userId → /users/:userId (parent root)
ts_compile(
    name = "routes",
    ...
)
```

See [Gazelle overview](../gazelle/overview.md#framework-detection) for what it
refuses and why.
