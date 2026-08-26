# Tailwind v4

Tailwind v4 has no config file of its own — it is a Vite plugin. So it goes in
through `vite_config`, the same seam any other Vite plugin uses, and nothing
about the rules is Tailwind-specific:

```typescript
// tailwind_config.ts
import tailwindcss from '@tailwindcss/vite';
import type { UserConfig } from 'vite';

const config: UserConfig = { plugins: [tailwindcss()] };

export default config;
```

```python
node_modules(
    name = "node_modules",
    deps = [
        "@npm_tailwind//:tailwindcss",
        "@npm_tailwind//:tailwindcss_vite",
        "@npm_tailwind//:vite",
    ],
)

vite_bundler(
    name = "vite",
    node_modules = ":node_modules",
    vite = "@npm_tailwind//:vite",
)

ts_bundle(
    name = "app",
    bundler = ":vite",
    entry_point = ":entry",
    html = "index.html",
    mode = "app",
    vite_config = "tailwind_config.ts",
)
```

All three packages have to be in the tree, not just the plugin:
`@tailwindcss/vite` reaches `tailwindcss` and `@tailwindcss/oxide` by bare
specifier from the staged config, and `@tailwindcss/oxide` is a
platform-specific native module — the lockfile carries twelve platform builds of
it beside the wrapper package, and the npm tree places the one this host can
load.

`//tests/tailwind` is the worked example, and it covers app mode, lib mode, and
the dev server under both implementations.

## Two things Bazel changes about scanning

Tailwind v4 generates a rule only for a class name it *finds*, and where it looks
is not where you would guess under Bazel.

**In app mode, the scan root is the HTML staging directory.** `@tailwindcss/vite`
scans from Vite's resolved `root`, not by walking up from the stylesheet — and in
app mode `ts_bundle` sets that root to the directory holding the staged HTML.
That directory holds `index.html` and nothing else, so the files to scan have to
be named:

```css
@import "tailwindcss";
@source "./entry.js";
```

`entry.js`, not `entry.ts`: the sandbox contains the **compiled** output, and it
still carries the class-name strings. A class that only ever appeared in a type
position would not survive into it, so that is the one shape this does not reach.

**Under the dev server the root is the workspace root**, which makes the
`@source` line redundant there but harmless — one stylesheet works in both modes.

## `@import "tailwindcss"` and NODE_PATH

`@tailwindcss/node` resolves that import from the CSS file's own directory with
[enhanced-resolve](https://github.com/webpack/enhanced-resolve), which is not
Node's ESM resolver. In dev the stylesheet is served out of the source tree, and
there is no `node_modules` above a source file to walk up to — the tree is a
Bazel output somewhere else entirely.

What makes it resolve is `NODE_PATH`, which enhanced-resolve reads and the
launcher sets to the npm tree. Nothing in the config expresses this, which is why
it is worth knowing: the failure is a `bazel build` that stays green and a dev
server that answers **500 for the stylesheet** and serves an unstyled page.

## Lib mode emits the stylesheet, and only because Bazel declares it

Lib mode puts every extracted rule in one `<bundle_name>.css` and never
references it from the JS, so nothing about the `.js` output implies the file
exists:

```python
ts_bundle(
    name = "lib",
    bundle_name = "lib",
    bundler = ":vite",
    entry_point = ":entry",
    format = "esm",
    vite_config = "tailwind_config.ts",
)
```

Vite writes it either way; an **undeclared** output is discarded with the
sandbox, which is what once made this look like Vite dropping CSS in lib mode.
`ts_bundle` declares it, so the consumer of the library gets it and has to include
it themselves. See
[Bundling § CSS](bundling.md#css-css-modules-and-assets) for the rest of that
distinction.

## Under oj

The dev server is a per-target choice and Tailwind is exercised under both:

```python
ts_dev_server(
    name = "dev_oj",
    entry_point = ":entry",
    node_modules = ":node_modules",
    server = "//oj:dev_server",
    vite_config = "tailwind_config.ts",
)
```

This is not a given the way most plugins are. `@tailwindcss/vite` is built on
Vite-only APIs — a `ResolvedConfig` handed to `configResolved`,
`config.createResolver` — so whether it runs at all in another server's plugin
host is a question rather than an assumption, and
`//tests/tailwind:tailwind_dev_oj_test` is what answers it. A plugin reaching for
an API oj's host does not supply is the failure mode to expect when swapping the
server under a framework plugin; see
[Choosing the server](dev-server.md#choosing-the-server).

## Its own npm hub

`//tests/tailwind` translates its own lockfile rather than adding Tailwind to the
main one, and the reason generalises: twelve platform-gated `oxide` packages
resolved into a curated fixture lockfile would have to be re-resolved by a
`pnpm add` that fixture cannot survive. A second hub costs a Gazelle directive
per package and its own `ts_add_package` target —
[More than one hub](npm.md#more-than-one-hub).
