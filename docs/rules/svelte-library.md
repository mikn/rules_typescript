# svelte_library

Compiles `.svelte` components with the Svelte compiler. A `.svelte` file is not
TypeScript, so `ts_compile` cannot read it; `svelte_library` is the rule that
turns one into files the rest of the graph can carry.

## Usage

```python
load("@rules_typescript//npm:defs.bzl", "node_modules")
load("@rules_typescript//ts:defs.bzl", "svelte_library")

node_modules(
    name = "node_modules",
    deps = ["@npm//:svelte"],
)

svelte_library(
    name = "components",
    srcs = [
        "src/Card.svelte",
        "src/nested/Plain.svelte",
    ],
    node_modules = ":node_modules",
)
```

The compiler is not vendored: it is loaded from the `node_modules` tree the
target names, so the Svelte version is the one in your lockfile.

## Attributes

| Attribute | Type | Default | Description |
|-----------|------|---------|-------------|
| `srcs` | `label_list` | — | Svelte component sources (`*.svelte`). Required. |
| `deps` | `label_list` | `[]` | Targets whose JavaScript and CSS the components import. Forwarded transitively through `JsInfo` and `CssInfo`. |
| `node_modules` | `label` | — | A `node_modules()` tree containing `svelte`. Required. |
| `generate` | `string` | `"client"` | Which compiler output to emit: `"client"` (browser) or `"server"` (SSR). |
| `dev` | `bool` | `False` | Compile with the compiler's `dev` option — runtime checks and source locations. |

## Outputs

Each source produces three declared files beside it in `bazel-bin`:

```
src/Card.svelte  →  src/Card.svelte.js      (JsInfo.js_files)
                    src/Card.svelte.js.map  (JsInfo.js_map_files)
                    src/Card.svelte.css     (CssInfo.css_files)
```

All three come out of **one** action per component. That is not an
implementation detail: Svelte scopes a component's CSS with a class whose hash
also appears in its JavaScript, so JS and CSS emitted by two actions would
eventually disagree about the class name and the styles would stop applying.

The output name keeps the source extension, so the import specifier does too:

```ts
import Card from "./Card.svelte.js";
```

## Two things worth knowing before you use it

**The `.css` is always there, and is empty for a component with no `<style>`.**
Whether a component has styles is not knowable at analysis time, and a declared
Bazel output has to exist, so the rule writes an empty file instead of guessing.

**One source belongs to one target.** Outputs are declared beside the source, so
two `svelte_library` targets in the same package cannot compile the same file
under different `generate` settings — they would declare the same output twice.
This is the same constraint `ts_compile` has for a `.ts` file.

## `<script lang="ts">`

The Svelte compiler strips types itself: annotations, `interface`, `type`,
generics, `as`, and type-only imports all come out as JavaScript. It is not a
TypeScript compiler, though, and rejects anything needing runtime emit — `enum`
and parameter properties among them — with its `typescript_invalid_feature`
error, which fails the build.

Two related gaps:

- No `.d.ts` is emitted, so a `.ts` file importing a component does not
  type-check against its props. Generating one needs `svelte2tsx`, which the
  `svelte` package does not ship.
- The types inside a `<script lang="ts">` block are stripped, not checked. That
  needs `svelte-check`.
