# remix_build

Wraps `remix vite:build` as a single Bazel action and returns both halves of a
Remix v2 application: the browser bundle under `client/` and the request handler
under `server/`.

The action stages a Remix project directory from declared inputs alone: the
`app/` sources, a `node_modules` tree, the Vite config carrying the Remix
plugin, an optional `tsconfig.json`. It runs the build in that directory and
moves Remix's build directory into one declared output artifact.

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

The config is a plain Remix config with no Bazel-specific lines: the rule builds
inside a staging root it owns and moves the result out afterwards.

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
| `srcs` | `label_list` | required | Every project file Remix reads: `app/root.tsx`, `app/entry.client.tsx`, `app/entry.server.tsx`, `app/routes/**`, and anything they import from inside this package |
| `node_modules` | `label` | required | A [`node_modules`](node-modules.md) target holding `@remix-run/dev` and the app's dependencies |
| `config` | `label` | required | The Vite config whose default export carries the Remix plugin |
| `config_srcs` | `label_list` | `[]` | Local modules `config` imports, staged at their paths relative to this package |
| `staging_srcs` | `label_list` | `[]` | Files from other packages, staged at their workspace-relative paths |
| `tsconfig` | `label` | `None` | A `tsconfig.json` staged at the project root |
| `env` | `string_dict` | `{}` | Extra environment variables for the build |

Each `srcs` file lands at its path relative to the target's package. Remix loads
`config` itself, so every Vite option in it reaches the build; it must not set
the plugin's `buildDirectory`, which the rule owns. `config_srcs` makes a
`./plugins/foo` import from the config resolve. `staging_srcs` has the same
contract as [`next_build`](next-build.md)'s.

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

## Two Vite Builds

`remix vite:build` loads the Vite config to read the Remix plugin's options,
then runs `vite build` twice against that config: once with `build.ssr = false`,
once with `build.ssr = true`. The plugin's `config` hook replaces `build.outDir`
and `build.rollupOptions.input` differently for each half.

The SSR half's `writeBundle` reads the SSR Vite manifest and moves the
SSR-emitted assets into the client directory. The browser asset manifest
`client/assets/manifest-<hash>.js` (the `window.__remixManifest` a hydrating
client reads) is emitted by the server build. Neither half is cacheable on its
own, so both come out of one action; a build that declares only `client/` as its
output produces a bundle that cannot boot.

[`ts_bundle`](ts-bundle.md)'s Vite wrapper runs exactly one
`vite build --config`, and the config it generates reads only `plugins` and
`root` out of a user `vite_config`, failing on any other key. `config` here is
the file Remix itself loads.

## SPA Mode

A config with `ssr: false` produces no server half:

- a `loader` export is a build error (`SPA Mode: N invalid route export(s)`);
- a resource route compiles to an empty client chunk;
- the server directory is deleted after `index.html` is prerendered.

`remix_build` fails and names the missing half. A client-only bundle is a
[`ts_bundle`](ts-bundle.md) with `vite_config`.

## Testing the Server Half

The server bundle imports `@remix-run/react`, `react-dom/server` and
`react/jsx-runtime` as bare specifiers and is ESM in a plain `.js`, so running
it needs the `node_modules` tree above it and a `package.json` saying
`"type": "module"`.

`//tests/integration:remix_ssr_test` copies `server/` next to the tree, imports
it through `createRequestHandler` from `@remix-run/node`, and asserts on the
responses: loader data in the HTML and in `window.__remixContext.loaderData`, a
nested layout and its child each running their own loader, a `POST` whose action
echoes the submitted form field, and a resource route answering `text/plain`
with no HTML shell.

## Route Conventions

Remix derives the route tree from filenames in `app/routes`, one directory level
deep. Gazelle's Remix plugin reads the same conventions and states the tree in
the generated BUILD file:

```python
# Remix route routes/_index → / (index, parent root)
# Remix route routes/dash.settings → /dash/settings (parent routes/dash)
# Remix route routes/users.$userId → /users/:userId (parent root)
ts_compile(
    name = "routes",
    ...
)
```

See [Gazelle overview](../gazelle/overview.md#framework-detection).
