# Tailwind v4

Tailwind v4 is a Vite plugin with no config file of its own, and goes in through
`vite_config` like any Vite plugin. Nothing in the rules is Tailwind-specific.

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

All three packages have to be in the tree. `@tailwindcss/vite` reaches
`tailwindcss` and `@tailwindcss/oxide` by bare specifier from the staged config,
and `@tailwindcss/oxide` is a platform-specific native module: the lockfile
carries twelve platform builds of it beside the wrapper package, and the npm tree
places the one this host can load.

`//tests/tailwind` is the worked example: app mode, lib mode, and the dev server
under both implementations.

## Class-Name Scanning

Tailwind v4 generates a rule only for a class name it finds, and where it looks
differs under Bazel.

In app mode the scan root is the HTML staging directory. `@tailwindcss/vite`
scans from Vite's resolved `root`, not the stylesheet's own directory, and
`ts_bundle` sets that root to the directory holding the staged HTML. It holds
`index.html` and nothing else, so the files to scan have to be named:

```css
@import "tailwindcss";
@source "./entry.js";
```

`entry.js`, not `entry.ts`: the sandbox holds the compiled output, which still
carries the class-name strings. A class that only ever appeared in a type
position does not survive into it.

Under the dev server the root is the workspace root, so the `@source` line is
redundant there but harmless: one stylesheet works in both modes.

## `@import "tailwindcss"` and NODE_PATH

`@tailwindcss/node` resolves that import from the CSS file's own directory with
[enhanced-resolve](https://github.com/webpack/enhanced-resolve), which is not
Node's ESM resolver. In dev the stylesheet is served out of the source tree, with
no `node_modules` above it to walk up to.

enhanced-resolve reads `NODE_PATH`, and the dev server launcher prepends the npm
tree to it. Nothing in the config expresses this. Without it `bazel build` stays
green and the dev server answers 500 for the stylesheet, so the page renders
unstyled.

## Lib Mode

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

Vite writes it either way, and an undeclared output is discarded with the
sandbox. `ts_bundle` declares it, so the library's consumer gets the file and
includes it. See [Bundling § CSS](bundling.md#css-css-modules-and-assets).

## Under oj

The dev server is a per-target choice; `//tests/tailwind` runs Tailwind under
both:

```python
ts_dev_server(
    name = "dev_oj",
    entry_point = ":entry",
    node_modules = ":node_modules",
    server = "//oj:dev_server",
    vite_config = "tailwind_config.ts",
)
```

`@tailwindcss/vite` is built on Vite-only APIs: a `ResolvedConfig` handed to
`configResolved`, and `config.createResolver`. Whether a plugin built on them
runs in another server's plugin host is decided per plugin.
`//tests/tailwind:tailwind_dev_oj_test` starts the target above and asserts that
the served stylesheet carries the generated rules. See
[Choosing the server](dev-server.md#choosing-the-server).

## Tailwind's npm Hub

Tailwind lives in `//tests/tailwind`'s own translated lockfile: twelve
platform-gated `oxide` packages would otherwise be resolved into a curated
fixture lockfile by a `pnpm add` that fixture cannot survive. A second hub costs
a Gazelle directive per package and its own `ts_add_package` target. See
[More than one hub](npm.md#more-than-one-hub).
