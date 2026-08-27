# examples/tanstack-app

A TanStack Start app — file-based routes, a server function, and a Zod
`validateSearch` — built by Bazel into the client and server halves the framework
emits.

`@tanstack/react-start` has been a plain Vite plugin since 1.121: there is no
Vinxi and no Nitro in its dependency closure. So there is no `tanstack_build`
rule here and there does not need to be one. `ts_bundle` with `staging_srcs` and
a `vite_config` is the whole integration.

## What this demonstrates

- Gazelle detecting TanStack Start from `package.json` and generating the bundle
  wiring: `node_modules`, `vite_bundler`, `ts_bundle`, and a `sources` filegroup
  per staged directory
- `staging_srcs`: the sources are copied into a writable directory inside the
  action sandbox, which becomes the Vite root, so the plugin's route generator
  can write `routeTree.gen.ts` where Bazel allows writes
- Both of the plugin's Vite environments landing in the one directory `ts_bundle`
  declares — `client/` and `server/`
- A server function: split into its own server-only module, keyed in the server
  bundle's resolver, and callable at `/_serverFn/<id>`
- Zod `validateSearch` on a route — a runtime concern the build needs nothing for
- vitest over the router factory, and tsgo type-checking every route

## Quick start

```bash
bazel build //...     # compile + type-check (validation is on via .bazelrc)
bazel test //...      # vitest, and the route-tree staleness test
bazel build //:app    # client/ + server/ under bazel-bin/app_bundle
bazel run //:gazelle  # regenerate BUILD files
```

Adding a route takes one more step — see "Adding a route" below.

## Layout

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

## What the build produces

```
bazel-bin/app_bundle/
  client/assets/main-<hash>.js                           # client entry
  client/assets/{routes,about,users}-<hash>.js            # one chunk per route
  server/server.mjs                                       # { fetch(Request) }
  server/assets/<route>-<hash>.mjs                        # server route chunks
  server/assets/_tanstack-start-manifest_v-<hash>.mjs     # the route manifest
```

`server/server.mjs` is a request handler, not a listening server, and it imports
`react`, `@tanstack/*` and `h3-v2` as bare specifiers — so it only runs with an
npm tree above it. Inside `bazel-bin` there is one, which is how
`//tests/integration:tanstack_test` fetches from it.

## Adding a route

```bash
# 1. write src/routes/<name>.tsx
bazel run //:gazelle                       # put it in the routes target
bazel run //src/routes:update_route_tree   # regenerate the route tree
bazel build //...
```

Step 2 is not optional, and `bazel test //...` is what tells you so: the
`//src/routes:route_tree_test` staleness test fails and names the command. Skip
it and the type-check fails instead, on the new route's path string:

```
error TS2345: Argument of type '"/users/$userId"' is not assignable to
              parameter of type 'keyof FileRoutesByPath | undefined'.
```

A route in a **subdirectory** of `src/routes` takes three hand-edits instead,
because Gazelle would make it a Bazel package and `:route_tree`'s
`glob(["**/*.tsx"])` cannot see into one — so the route would type-check against
nothing, be absent from the bundle, and leave `:route_tree_test` green:

- `# gazelle:exclude <dir>` in `src/routes/BUILD.bazel`, so no BUILD file is
  written there and the glob reaches the files
- `"<dir>/<name>.tsx",  # keep` in the `:routes` `srcs`
- the same line in the `:sources` `filegroup`, which is what stages it

## The route tree

Two consumers want `routeTree.gen.ts`, and only one of them can be handed a
generated file.

The **bundle** needs nothing checked in: the Start plugin's own generator writes
the tree into the staging directory on every build, and
`tanstack-vite.config.mjs` points `generatedRouteTree` at the path the staged
`src/lib/router.ts` imports it from.

The **type-check** cannot use that copy. `src/lib/router.ts` imports the tree,
and each route's `createFileRoute("/about")` is typed by the `declare module`
block inside it — so the tree and the routes have to be in one `ts_compile`, and
`ts_compile` refuses a target mixing checked-in and generated sources (one
`rootDir` per declaration emit). The routes cannot move to `declarations =
"oxc"` either: `export const Route = createFileRoute(...)({...})` has no
explicit type, which isolated declarations require.

So the tree is checked in at `src/routes/routeTree.gen.ts`, listed in `srcs`
behind a `# keep` (Gazelle drops every `*.gen.ts` from a source target), and
three targets in `src/routes/BUILD.bazel` keep it honest:

| target                | what it does                                            |
| --------------------- | ------------------------------------------------------- |
| `:route_tree`         | `ts_codegen` running `@tanstack/router-generator`        |
| `:update_route_tree`  | `bazel run` copies that output over the checked-in file  |
| `:route_tree_test`    | fails when the two differ, naming the command above      |

The generator itself ships with the ruleset —
`@rules_typescript//tools/codegen:tanstack_routes` — so the only per-workspace
wiring is the `node_modules` target giving it `@tanstack/router-generator`.

## index.html and src/app/main.tsx are inert

`ts_bundle` in app mode requires an `html` and an `entry_point`. TanStack Start
uses neither: it overrides the client environment's Rollup input with
`virtual:tanstack-start-client-entry`, and the document comes from the root
route's `shellComponent`, so no HTML is emitted at all. Both files stay because
the rule demands them; nothing in the output comes from either.

## Hermeticity: the path-stability plugin

The plugin bakes each route file's **absolute** path into the route-manifest
module. Under Bazel that path is the per-action sandbox, so the manifest chunk's
content — and therefore its hashed filename — moves on every build. Two
consecutive builds emit two differently-named manifest chunks.

`tanstack-vite.config.mjs` adds a ten-line plugin with `enforce: "post"` whose
`transform` strips the Vite root prefix from that one module. `transform` runs
before hashing, so both the content and the filename settle; a `sed` over the
output directory could not fix it, because the name is already derived from the
pre-scrub content. Nothing at runtime reads the manifest's `filePath` — it is
used at build time to key route chunks — so scrubbing it is safe.

`//tests/integration:tanstack_test` asserts the manifest carries no absolute
path, and, with the plugin removed, that it does.

## Not here yet

`bazel run` on a Start dev server does not work against a Bazel-only npm tree:
Vite's SSR module runner inlines `react/jsx-runtime` instead of externalizing it
(the npm tree is a build output, not a directory the importer can walk up to),
and React's CJS entry then evaluates `module` in an ESM context. The dev server
answers 500 on every request. So there is no dev-server target here — Gazelle
names that refusal when it walks the workspace rather than generating one that
only looks like it worked in a checkout that happens to also have a pnpm
`node_modules/`.

## Using as a template

Copy this directory. Remove the `local_path_override` block in `MODULE.bazel` and
set the `rules_typescript` version to the published one. Keep `pnpm-lock.yaml`
checked in — run `pnpm install` to update it when adding npm dependencies.

`src/routes/BUILD.bazel` is the part to copy verbatim: the `ts_codegen`,
`refresh_workspace_files` and `diff_test` trio is what makes adding a route a
named step instead of a type error.
