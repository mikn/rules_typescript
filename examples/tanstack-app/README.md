# examples/tanstack-app

A TanStack Start app — file-based routes, a server function, and a Zod
`validateSearch` — built by Bazel into the client and server halves the framework
emits.

`@tanstack/react-start` has been a plain Vite plugin since 1.121, with no Vinxi
and no Nitro in its dependency closure. There is no `tanstack_build` rule here:
`ts_bundle` with `staging_srcs` and a `vite_config` is the whole integration.

## What This Demonstrates

- Gazelle detecting TanStack Start from `package.json` and generating the bundle
  wiring: `node_modules`, `vite_bundler`, `ts_bundle`, and a `sources` filegroup
  per staged directory
- `staging_srcs`: sources are copied into a writable directory inside the action
  sandbox, which becomes the Vite root, so the plugin's route generator can write
  `routeTree.gen.ts`
- The plugin's two Vite environments, `client/` and `server/`, landing in the one
  directory `ts_bundle` declares
- A server function: split into its own server-only module, keyed in the server
  bundle's resolver, and callable at `/_serverFn/<id>`
- Zod `validateSearch` on a route: a runtime concern the build needs nothing for
- vitest over the router factory, and tsgo type-checking every route

## Quick Start

```bash
bazel build //...     # compile + type-check (validation is on via .bazelrc)
bazel test //...      # vitest, and the route-tree staleness test
bazel build //:app    # client/ + server/ under bazel-bin/app_bundle
bazel run //:gazelle  # regenerate BUILD files
```

## Structure

```
examples/tanstack-app/
  index.html                  # inert: see below
  tanstack-vite.config.mjs    # tanstackStart() + the path-stability plugin
  BUILD.bazel                 # node_modules, vite_bundler, ts_bundle
  src/
    routes/
      __root.tsx              # createRootRoute with the document shell
      index.tsx               # createFileRoute("/") + createServerFn loader
      about.tsx               # createFileRoute("/about")
      users.tsx               # createFileRoute("/users") + Zod validateSearch
      routeTree.gen.ts        # checked in and generated; see "The route tree"
    lib/
      router.ts               # the router entry the plugin calls per request
      params.ts               # standalone Zod schemas
    components/               # Layout, UserCard
    app/
      index.ts                # re-exports getRouter
      main.tsx                # inert: see below
```

## Build Output

```
bazel-bin/app_bundle/
  client/assets/main-<hash>.js                           # client entry
  client/assets/{routes,about,users}-<hash>.js            # one chunk per route
  server/server.mjs                                       # { fetch(Request) }
  server/assets/<route>-<hash>.mjs                        # server route chunks
  server/assets/_tanstack-start-manifest_v-<hash>.mjs     # the route manifest
```

`server/server.mjs` exports a request handler; it does not listen on a port. It
imports `react`, `@tanstack/*` and `h3-v2` as bare specifiers, so it needs an npm
tree above it. `bazel-bin` has one, and `//tests/integration:tanstack_test`
fetches from the handler there.

## Adding a Route

```bash
# 1. write src/routes/<name>.tsx
bazel run //:gazelle                       # put it in the routes target
bazel run //src/routes:update_route_tree   # regenerate the route tree
bazel build //...
```

`//src/routes:route_tree_test` fails when `update_route_tree` is skipped, and
names the command. The type-check then fails on the new route's path string:

```
error TS2345: Argument of type '"/users/$userId"' is not assignable to
              parameter of type 'keyof FileRoutesByPath | undefined'.
```

A route in a subdirectory of `src/routes` takes three hand-edits, because
Gazelle would make the subdirectory a Bazel package and `:route_tree`'s
`glob(["**/*.tsx"])` cannot see into one. The route would type-check against
nothing, be absent from the bundle, and leave `:route_tree_test` green. The
edits:

- `# gazelle:exclude <dir>` in `src/routes/BUILD.bazel`, so no BUILD file is
  written there and the glob reaches the files
- `"<dir>/<name>.tsx",  # keep` in the `:routes` `srcs`
- the same line in the `:sources` `filegroup`, which is what stages it

## The Route Tree

Two things consume `routeTree.gen.ts`: the bundle and the type-check.

The **bundle** needs nothing checked in: the Start plugin's own generator writes
the tree into the staging directory on every build, and
`tanstack-vite.config.mjs` points `generatedRouteTree` at the path the staged
`src/lib/router.ts` imports it from.

The **type-check** cannot use that copy. `src/lib/router.ts` imports the tree,
and each route's `createFileRoute("/about")` is typed by the `declare module`
block inside it, so the tree and the routes have to be in one `ts_compile`.
`ts_compile` rejects a target mixing checked-in and generated sources — one
`rootDir` per declaration emit. The routes cannot move to `declarations = "oxc"`
either: `export const Route = createFileRoute(...)({...})` has no explicit type,
which isolated declarations require.

The tree is checked in at `src/routes/routeTree.gen.ts` and listed in `srcs`
behind a `# keep`, because Gazelle drops every `*.gen.ts` from a source target.
Three targets in `src/routes/BUILD.bazel` keep the two copies in sync:

| target                | what it does                                            |
| --------------------- | ------------------------------------------------------- |
| `:route_tree`         | `ts_codegen` running `@tanstack/router-generator`        |
| `:update_route_tree`  | `bazel run` copies that output over the checked-in file  |
| `:route_tree_test`    | fails when the two differ, naming the command above      |

The generator ships with the ruleset as
`@rules_typescript//tools/codegen:tanstack_routes`. The only per-workspace wiring
is the `node_modules` target giving it `@tanstack/router-generator`.

## Inert Files

`ts_bundle` in app mode requires an `html` and an `entry_point`. TanStack Start
uses neither: it overrides the client environment's Rollup input with
`virtual:tanstack-start-client-entry`, and the document comes from the root
route's `shellComponent`, so no HTML is emitted at all. `index.html` and
`src/app/main.tsx` exist only to satisfy the rule.

## The Path-Stability Plugin

The Start plugin bakes each route file's absolute path into the route-manifest
module. Under Bazel that path is the per-action sandbox, so the chunk's content
and its hashed filename move on every build.

`tanstack-vite.config.mjs` adds a ten-line plugin with `enforce: "post"` whose
`transform` strips the Vite root prefix from that one module. `transform` runs
before hashing, so both the content and the filename settle. A `sed` over the
output directory would come too late: the name is already derived from the
pre-scrub content. The manifest's `filePath` is read at build time to key route
chunks and by nothing at runtime, so scrubbing it is safe.

`//tests/integration:tanstack_test` checks the manifest for an absolute path,
with the plugin in place and with it removed.

## Dev Server

```bash
bazel run //:dev        # Vite, http://localhost:5173, SSR and all
bazel run //:dev_oj     # the same app under oj, http://localhost:5174
```

`//:dev` takes the same `vite_config` as `//:app`, so the Start plugin owns
routing, the server functions and the client entry exactly as it does in the
build. Gazelle generates it beside `ts_bundle` for that reason: a per-package dev
target would not have the config, and without the plugin there is no app.

Starting it links the npm tree in as `node_modules` at the workspace root, and
removes the link on Ctrl-C. Vite decides SSR externalisation and
`optimizeDeps.include` without consulting any plugin — both walk up from the
importer, or from the Vite root — so a tree that is only a Bazel output is
invisible to them, and `react/jsx-runtime` gets inlined as CJS into an ESM
evaluator. The link is what makes both resolve. See
[the dev server guide](../../docs/guides/dev-server.md#how-a-bare-npm-specifier-resolves).

### Two files that name this app's layout

This app does not use the framework's default paths: the router entry is
`src/lib/router.ts` (so the lib package owns it and its test) and the route tree
is `src/routes/routeTree.gen.ts` (so the tree and the routes it types are one
`ts_compile`). Vite learns both from `tanstack-vite.config.mjs`. Two other
readers cannot see that file, so they are told directly:

| file | reader | what it says |
| --- | --- | --- |
| `tsr.config.json` | `@tanstack/router-generator` | where the routes are and where the tree goes |
| `package.json` `imports` | any bundler resolving `#tanstack-router-entry` | where `getRouter` is |

Both are standard framework/Node mechanisms rather than anything this ruleset
invented, and Vite ignores them because the plugin's own options win.

oj detects a TanStack Start app from `src/routes` plus the dependency and serves
it through its own adapter rather than the Vite plugin -- a second implementation
of the same app, which is what makes the two files above worth having: they are
the only description of this layout that both can read.

The Start plugin regenerates `src/routes/routeTree.gen.ts` while it serves.
`:route_tree` passes `--start-router` so it emits the same `declare module`
footer, which makes that write a no-op and keeps `:route_tree_test` green.

## Using as a Template

Copy this directory. Remove the `local_path_override` block in `MODULE.bazel` and
set the `rules_typescript` version to the published one. Keep `pnpm-lock.yaml`
checked in — run `pnpm install` to update it when adding npm dependencies.

Copy `src/routes/BUILD.bazel` verbatim: its `ts_codegen`,
`refresh_workspace_files` and `diff_test` targets are what make adding a route a
named step.
