# Providers and Toolchains

The contract a rule outside this ruleset writes against: the providers the
rules return, and the toolchains they resolve. Seven providers load from
`@rules_typescript//ts:defs.bzl`; the toolchain contract loads from
`@rules_typescript//ts/toolchain:defs.bzl`.

```python
load(
    "@rules_typescript//ts:defs.bzl",
    "AssetInfo",
    "BundlerInfo",
    "CssInfo",
    "CssModuleInfo",
    "JsInfo",
    "TsDeclarationInfo",
    "TsLintInfo",
)
```

| Provider | Returned by |
|---|---|
| `JsInfo` | `ts_compile`, `ts_codegen`, `ts_binary`, the `@npm` package targets |
| `TsDeclarationInfo` | `ts_compile`, `ts_codegen`, `css_library`, `css_module`, `asset_library`, `json_library`, the `@npm` package targets |
| `CssInfo` | `css_library`; `ts_compile` with empty direct fields, so a consumer reads its transitive ones |
| `CssModuleInfo` | `css_module`; `ts_compile` with empty direct fields |
| `AssetInfo` | `asset_library`; `ts_compile` with empty direct fields |
| `BundlerInfo` | any rule that [brings its own bundler](../guides/bundling.md#custom-bundler-bundlerinfo-interface); the ruleset ships none |
| `TsLintInfo` | `ts_lint` |

## Direct and Transitive Fields

A direct field carries only what the target itself produces. A rule that
forwards a dep's files leaves the direct field empty and puts the closure in the
transitive one; `ts_compile` does that for the CSS and assets its deps carry. A
consumer that wants everything reachable reads the transitive field.

## JsInfo

| Field | Type | Description |
|---|---|---|
| `js_files` | `depset of File` | The `.js` files this target produces: compiled output, plus any JavaScript src staged as-is |
| `js_map_files` | `depset of File` | The `.js.map` files this target produces |
| `transitive_js_files` | `depset of File` | Every `.js` from this target and its deps |
| `transitive_js_map_files` | `depset of File` | Every `.js.map` from this target and its deps |

`ts_binary` with a `bundler` returns the bundle as the one member of all four.
`ts_codegen` with `out_dir` returns the directory as the one member; nothing
downstream compiles the tree, so what it holds is already compiled output. An
`outs` codegen returns its `.js` outs here and its `.d.ts` outs in
`TsDeclarationInfo`, so `deps = [":worker_types"]` is legal and a consumer's
tsconfig `types` can name the generated declaration.

## TsDeclarationInfo

| Field | Type | Description |
|---|---|---|
| `declaration_files` | `depset of File` | The declarations this target produces, plus the ambient ones it passes through from `srcs`. A global one is in scope in a consumer only when the consumer's tsconfig `types` names it |
| `transitive_declaration_files` | `depset of File` | Every declaration from this target and its first-party deps. An npm package's declarations reach a consumer through the node_modules forest its tsgo action stages, not through this depset |
| `transitive_npm_packages` | `depset of NpmPackageInfo` | The npm packages a consumer links into its forest for this target's declarations. A dep's emitted `.d.ts` imports the packages the dep declared and resolves them in the consumer's program by walking that forest. An npm package target names its `transitive_deps`; the package itself arrives through its `NpmPackageInfo` |

A global `.d.ts` travels as a declaration output and nothing more: the consumer
names it in its own tsconfig `types` to bring its globals into scope. See
[Which ambients a consumer gets](ts-compile.md#which-ambients-a-consumer-gets).

## CssInfo

| Field | Type | Description |
|---|---|---|
| `css_files` | `depset of File` | The `.css` files this target itself produces; empty on a target that only forwards them |
| `transitive_css_files` | `depset of File` | Every `.css` reachable from this target |

## CssModuleInfo

| Field | Type | Description |
|---|---|---|
| `css_files` | `depset of File` | The `.module.css` files this target itself produces; empty on a target that only forwards them |
| `transitive_css_files` | `depset of File` | Every `.module.css` reachable from this target |
| `exports_files` | `depset of File` | One `<source>.exports.json` per direct src: the scoped-name map postcss-modules produced. Its keys are what the `.d.ts` declares and its values the class names the bundler emits |
| `transitive_exports_files` | `depset of File` | Every `.exports.json` reachable from this target |

## AssetInfo

| Field | Type | Description |
|---|---|---|
| `asset_files` | `depset of File` | The asset files this target itself produces; empty on a target that only forwards them |
| `transitive_asset_files` | `depset of File` | Every asset reachable from this target |

## BundlerInfo

| Field | Type | Description |
|---|---|---|
| `bundler_binary` | `File` | The bundler executable |
| `config_file` | `File or None` | A static config passed as `--config`, in mode 1 only |
| `runtime_deps` | `depset of File` | Files the bundler needs at run time |
| `use_generated_config` | `bool` | `True` selects mode 2: `ts_binary` generates a `vite.config.mjs` and passes it with the entry, the output directory and the stylesheet. Default `False` |

The two invocation modes and the recipe for a bundler of your own are in
[Bundling](../guides/bundling.md#custom-bundler-bundlerinfo-interface).

## TsLintInfo

| Field | Type | Description |
|---|---|---|
| `stamp` | `File` | The validation stamp, written only on a clean lint run |

## NpmPackageInfo

`NpmPackageInfo` is not exported from `@rules_typescript//ts:defs.bzl`. It loads
from `@rules_typescript//ts/private:providers.bzl`, and everything under
`ts/private/` is [volatile](../compatibility.md#volatile). Every `@npm` package
target returns it, and so does a workspace member's hub view
`npm_workspace_package`; the `node_modules` builder lays a tree out from it, the
runtime tree's and the type-check forest's alike.

| Field | Type | Description |
|---|---|---|
| `package_name` | `string` | The npm name, `react` or `@types/react`; what the tree links the package as |
| `package_version` | `string` | The version; `0.0.0` on a workspace member, which pnpm resolves by path |
| `peer_id` | `string` | A filesystem-safe token naming the peer set this resolution was made against, empty for a package pnpm resolved only one way. Two snapshots can share `name@version` and differ only here |
| `package_dir` | `File or None` | The `package.json` at the root of the extracted package. `None` on a workspace member, whose view writes the manifest it links |
| `package_root` | `string` | Exec-root-relative directory the files in `all_files` hang off: where `package_dir` sits for an extracted tarball, the member's directory under `bazel-bin` for a workspace member |
| `all_files` | `depset of File` | Every file of the package (`package.json`, `.js`, `.d.ts`, other assets): what a `node_modules` tree holds for it |
| `js_files` | `depset of File` | The JavaScript files in the package |
| `direct_deps` | `list of NpmPackageInfo` | The packages this one depends on directly, each under the name this package imports it by; what places two versions of one name in a tree |
| `transitive_deps` | `depset of NpmPackageInfo` | Every npm package reachable from this one, the paired `@types/*` package included |
| `transitive_package_dirs` | `depset of File` | The `package.json` of this package and of every transitive dep |

The package's `exports`, `types` and `main` are nowhere in it: tsgo and node
read the manifest in the tree, as they do over an install.

## DevServerInfo

`DevServerInfo` is not exported either; it loads from the same private file.
The shipped implementation, `//vite:dev_server`, returns it;
`ts_dev_server(server = ...)` takes any target that does.

| Field | Type | Vite | Description |
|---|---|---|---|
| `server_binary` | `File or None` | `None` | The server executable, for a server that is a build artifact. `None` when it ships inside the npm tree |
| `server_in_tree` | `string` | `"vite/bin/vite.js"` | The executable's path relative to the root of the `node_modules` tree, for a server that ships as an npm package. Exactly one of the two is set |
| `argv` | `list of string` | `["dev", "--config", "{config}"]` | The command line after the executable. `{config}` expands to the generated config's path, `{port}` to the `port` attr, `{root}` to the directory served |
| `config_dialect` | `string` | `"vite"` | The config format the server is handed. Only `"vite"` is generated today; a server reading its own format declares its own dialect, and the generator has to learn it before that server can be selected |
| `runs_in_js_runtime` | `bool` | `True` | `True` when the executable is JavaScript and the toolchain Node runs it. A native server still gets the toolchain Node on `PATH`, for a plugin host that is a Node process |
| `ignored_config_fields` | `list of string` | `[]` | Dotted config paths the server does not honour. A target whose configuration reaches one fails at analysis time naming the field and the server |
| `native_react_refresh` | `bool` | `False` | `True` when the server applies React Fast Refresh itself. `react_refresh = True` then fails at analysis time |
| `runtime_deps` | `depset of File` | empty | Everything the server needs in runfiles beyond the generated config and the npm tree |

A server shipping as an npm package has no `File` to point at: its executable is
a path inside the `node_modules` tree artifact, which Starlark cannot address at
analysis time. That is why `server_in_tree` exists beside `server_binary`.

## Toolchain Contract

`@rules_typescript//ts/toolchain:defs.bzl` exports eleven names: four toolchain
type labels, three providers, and four accessors.

```python
load(
    "@rules_typescript//ts/toolchain:defs.bzl",
    "JS_RUNTIME_TOOLCHAIN_TYPE",
    "JS_TOOL_TOOLCHAIN_TYPE",
    "OXC_TOOLCHAIN_TYPE",
    "TSGO_TOOLCHAIN_TYPE",
    "JsRuntimeInfo",
    "OxcToolchainInfo",
    "TsgoToolchainInfo",
    "get_js_runtime",
    "get_js_tool",
    "get_oxc_toolchain",
    "get_tsgo_toolchain",
)
```

| Type label | Target | Runs on | Accessor | Returns |
|---|---|---|---|---|
| `OXC_TOOLCHAIN_TYPE` | `//ts/toolchain:oxc_toolchain_type` | the exec platform | `get_oxc_toolchain(ctx)` | `OxcToolchainInfo` |
| `TSGO_TOOLCHAIN_TYPE` | `//ts/toolchain:tsgo_toolchain_type` | the exec platform | `get_tsgo_toolchain(ctx)` | `TsgoToolchainInfo` |
| `JS_RUNTIME_TOOLCHAIN_TYPE` | `//ts/toolchain:js_runtime_type` | the target platform | `get_js_runtime(ctx)` | `JsRuntimeInfo`, or `None` when no toolchain resolved |
| `JS_TOOL_TOOLCHAIN_TYPE` | `//ts/toolchain:js_tool_type` | the exec platform | `get_js_tool(ctx)` | `JsRuntimeInfo`, or `None` when no toolchain resolved |

The labels are `Label()` values, so they resolve in this ruleset's own
repository mapping and keep working under another repository name.

Node fills two roles that resolve against different platforms. `js_runtime_type`
is the runtime a `ts_test` or `ts_binary` program executes on: it is built for
the target platform and staged into runfiles. `js_tool_type` is Node as a build
tool (the `node_modules` tree builder, `ts_codegen`, the bundlers): it runs on
the exec platform. The two are equal under a plain host
build and differ the moment `--platforms` does.

| Provider | Field | Type | Description |
|---|---|---|---|
| `OxcToolchainInfo` | `oxc_binary` | `File` | The oxc-bazel CLI binary |
| `TsgoToolchainInfo` | `tsgo_binary` | `File` | The tsgo CLI binary |
| `JsRuntimeInfo` | `runtime_binary` | `File` | The runtime executable: node, or a Deno, Bun or wrapper a consumer registers |
| | `runtime_name` | `string` | The name diagnostics use; `"node"` for the shipped toolchains |
| | `args_prefix` | `list of string` | Arguments placed before the entry script |

A rule declares the types it needs and reads them through the accessors:

```python
load(
    "@rules_typescript//ts/toolchain:defs.bzl",
    "JS_TOOL_TOOLCHAIN_TYPE",
    "TSGO_TOOLCHAIN_TYPE",
    "get_js_tool",
    "get_tsgo_toolchain",
)

def _impl(ctx):
    tsgo = get_tsgo_toolchain(ctx).tsgo_binary
    node = get_js_tool(ctx)
    ...

my_rule = rule(
    implementation = _impl,
    toolchains = [
        TSGO_TOOLCHAIN_TYPE,
        config_common.toolchain_type(JS_TOOL_TOOLCHAIN_TYPE, mandatory = False),
    ],
)
```

`get_oxc_toolchain` and `get_tsgo_toolchain` index `ctx.toolchains` directly, so
a rule listing either type as mandatory fails toolchain resolution when nothing
registers one. `get_js_runtime` and `get_js_tool` return `None` for a type
declared with `mandatory = False` that nothing registered; `ts_compile` declares
tsgo and the JS tool that way, and oxc as mandatory.

`register_toolchains("@rules_typescript//ts/toolchain:all")` registers every
instance: one oxc toolchain, built from source by rules_rust for whichever exec
platform runs the build; one tsgo toolchain per platform in `TSGO_PLATFORMS`,
constrained on the exec platform; one Node runtime toolchain per platform in
`NODE_PLATFORMS`, constrained on the target platform; and one Node tool
toolchain per platform, constrained on the exec platform.

Which compiler the tsgo toolchains hold is the `ts` extension's to say, and it
reads a pnpm lockfile: the root importer's `typescript` entry names the version,
and the `packages:` entries of that version's platform packages
(`@typescript/typescript-<os>-<cpu>`) carry the tarball and integrity of each
`lib/tsc`, which is what the per-platform repository rule downloads and
verifies. A consumer names its own lockfile:

```python
ts = use_extension("@rules_typescript//ts:extensions.bzl", "ts")
ts.tsgo(pnpm_lock = "//:pnpm-lock.yaml")
```

With no call the lockfile is rules_typescript's own
`ts/private/tsgo/pnpm-lock.yaml`. `ts.tsgo(version = "...")` is the alternative
for a release no lockfile states, downloaded unverified;
`package = "@typescript/native-preview"` selects the nightly, whose binary is
`lib/tsgo`. `TsgoToolchainInfo.tsgo_binary` is that file either way.

`//tests/toolchain:foreign_target_platform_test` pins the split. It analyses a
probe rule under `--platforms=//platforms:windows_amd64`, a platform with a Node
runtime and no compiler binary: oxc and tsgo still resolve to exec-platform
binaries, the staged runtime is `nodejs_windows_amd64`, and the tool runtime is
not.
