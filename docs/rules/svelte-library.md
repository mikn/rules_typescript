# svelte_library

Compiles `.svelte` components with the Svelte compiler. A `.svelte` file is not
TypeScript, so `ts_compile` cannot read it.

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

The compiler is loaded from the `node_modules` tree the target names, so the
Svelte version is the lockfile's.

## Attributes

| Attribute | Type | Default | Description |
|-----------|------|---------|-------------|
| `srcs` | `label_list` | required | Svelte component sources (`*.svelte`) |
| `deps` | `label_list` | `[]` | Targets whose JavaScript and CSS the components import. Forwarded transitively through `JsInfo` and `CssInfo`. |
| `node_modules` | `label` | required | A `node_modules()` tree containing `svelte` |
| `generate` | `string` | `"client"` | Which compiler output to emit: `"client"` (browser) or `"server"` (SSR). |
| `dev` | `bool` | `False` | Compile with the compiler's `dev` option: runtime checks and source locations. |

## Outputs

Each source produces three declared files beside it in `bazel-bin`:

```
src/Card.svelte  →  src/Card.svelte.js      (JsInfo.js_files)
                    src/Card.svelte.js.map  (JsInfo.js_map_files)
                    src/Card.svelte.css     (CssInfo.css_files)
```

All three come out of one action per component. Svelte scopes a component's
CSS with a class whose hash also appears in its JavaScript.

The `.css` is always present and is empty for a component with no `<style>`
block.

The output name keeps the source extension, so the import specifier does too:

```ts
import Card from "./Card.svelte.js";
```

## One Target per Source

Outputs are declared beside the source, so two `svelte_library` targets in the
same package cannot compile the same file under different `generate` settings:
they would declare the same output twice. `ts_compile` has the same constraint
for a `.ts` file.

## `<script lang="ts">`

The Svelte compiler strips types: annotations, `interface`, `type`, generics,
`as`, and type-only imports come out as JavaScript. Anything needing runtime
emit, `enum` and parameter properties among them, fails the build with its
`typescript_invalid_feature` error.

Two gaps:

- No `.d.ts` is emitted, so a `.ts` file importing a component does not
  type-check against its props. Generating one needs `svelte2tsx`, which the
  `svelte` package does not ship.
- The types inside a `<script lang="ts">` block are stripped, not checked. That
  needs `svelte-check`.
