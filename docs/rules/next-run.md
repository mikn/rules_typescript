# next_dev_server and next_serve

Two ways to run the app [`next_build`](next-build.md) compiles.

| Rule | Command | Project directory | What it is for |
| --- | --- | --- | --- |
| `next_dev_server` | `next dev` | your source tree | the inner loop: an edit reaches the response through Next.js's watcher, with no rebuild |
| `next_serve` | `next start` | a staged copy of the build | server-rendering the built app |

Neither rule generates a config. Next.js reads `next.config.*` from its project
directory; what the rules supply is the Bazel-built npm tree.

## Usage

```python
load("@rules_typescript//ts:defs.bzl", "next_build", "next_dev_server", "next_serve")
load("@rules_typescript//npm:defs.bzl", "node_modules")

node_modules(
    name = "node_modules",
    deps = ["@npm//:next", "@npm//:react", "@npm//:react-dom"],
)

next_build(
    name = "app",
    srcs = glob(["app/**", "pages/**", "public/**"]),
    config = "next.config.mjs",
    node_modules = ":node_modules",
)

next_dev_server(
    name = "dev",
    node_modules = ":node_modules",
    port = 3000,
)

next_serve(
    name = "serve",
    srcs = glob(["public/**"]),
    build = ":app",
    config = "next.config.mjs",
    node_modules = ":node_modules",
    port = 3000,
)
```

```bash
bazel run //:dev                      # next dev, over source
bazel run //:serve                    # next start, over the build
bazel run //:serve -- --port 41234    # any port, e.g. one a test reserved
```

Gazelle generates the `dev` target beside `next_build` and does not generate
`serve`: which files a served app needs beside `.next`, and on which port, is a
deployment decision.

## Attributes

Shared by both rules:

| Attribute | Meaning |
| --- | --- |
| `node_modules` | A `node_modules()` target carrying `next`, `react` and `react-dom`. Mandatory. |
| `port` | Default listen port. `--port N` past the launcher overrides it. |
| `env` | Environment variables for the server process. |
| `data` | Extra files to place in runfiles. |

`next_serve` adds:

| Attribute | Meaning |
| --- | --- |
| `build` | The `next_build` target whose `.next` directory to serve. Mandatory. |
| `config` | The same `next.config` file the build was given. |
| `srcs` | Files staged beside the build output at their package-relative paths. |

`next start` reads `config` for what applies at request time: rewrites, headers,
image domains, `basePath`.

## Module Resolution

Next.js seeds webpack's `resolve.modules` from `NODE_PATH`
(`next/dist/build/webpack-config.js`), and Node's own CJS resolution reads it
too. Both rules set it to the Bazel npm tree, so the app's bare imports resolve
there and no `node_modules` symlink is planted in a source directory.

## What `next_serve` stages

The staged project directory holds a **copy** of `.next`, not a symlink into
`bazel-bin`. The image optimizer writes into `.next/cache` when it serves
`/_next/image?url=…`, and a Bazel output tree is read-only. The copy is
discarded when the server exits.

`public/` is the other half: `next build` never copies it into `.next`, because
Next.js serves it from the project root at request time. It reaches the staged
directory through `srcs`, which is also where a module the config imports goes.

`next_serve` runs the build; it is not a deployment artifact. The deployable
unit is the build output plus the config, `public/` and the npm tree, which is
what the rule assembles at run time. `output: "standalone"` is untested here.

## What `next dev` writes

`next dev` treats its project directory as its own: it writes `.next/` and
`next-env.d.ts` there, and adds `.next/types/**/*.ts` to the `include` of
`tsconfig.json`. `distDir` is a `next.config` setting and the rule does not own
your config, so this is Next.js's own behaviour showing through. All three are
the paths a Next.js project gitignores anyway.

## Turbopack

Unsupported. `next dev --turbo` replaces the module resolution `NODE_PATH`
feeds, and nothing here is tested against it.

## Testing

`//tests/integration:nextjs_test` starts both servers on a kernel-assigned port
and asserts over HTTP:

- two requests to a `force-dynamic` route return different HTML: the route
  renders a per-request nonce. Before the servers start, the same test reads
  `prerender-manifest.json` out of the build — that route is absent from it
  while a sibling static route is present;
- the `Host` header the request supplied appears in the server-rendered HTML,
  for both the App Router (`headers()`) and the Pages Router
  (`getServerSideProps`);
- the middleware's response header is set, which only exists on a served
  response;
- both API-route flavours answer with their JSON;
- `/_next/image?url=…` answers with an image;
- for `next_dev_server`, a source file edited while the server runs is served
  on the next request, with no Bazel invocation in between.
