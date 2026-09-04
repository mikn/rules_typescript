# Providers and Toolchains

The contract a rule outside this ruleset writes against: the providers the
rules return, and the toolchains they resolve. Eight providers load from
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
    "TsModuleInfo",
)
```

| Provider | Returned by |
|---|---|
| `JsInfo` | `ts_compile`, `ts_codegen` (`out_dir`), `ts_binary`, the `@npm` package targets |
| `TsDeclarationInfo` | `ts_compile`, `ts_codegen` (`out_dir`), `css_library`, `css_module`, `asset_library`, `json_library`, the `@npm` package targets |
| `TsModuleInfo` | `ts_compile`, `ts_codegen` (`out_dir`) |
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

`ts_binary` with a `bundler` returns the bundle as the one member of all four. `ts_codegen` with
`out_dir` returns the directory as the one member; nothing downstream compiles
the tree, so what it holds is already compiled output.

## TsDeclarationInfo

| Field | Type | Description |
|---|---|---|
| `declaration_files` | `depset of File` | The declarations this target produces, plus the ambient ones it passes through from `srcs` |
| `transitive_declaration_files` | `depset of File` | Every declaration from this target and its deps |
| `transitive_npm_packages` | `depset of NpmPackageInfo` | The npm packages whose declarations are in `transitive_declaration_files`. A dep's emitted `.d.ts` imports the packages the dep declared and resolves them in the consumer's program, so the consumer writes a `paths` key for each. An npm package target names its `transitive_deps`; the package itself arrives through its `NpmPackageInfo` |
| `global_entry_files` | `depset of File` | A generated `.d.ts` referencing the srcs `public_globals` names, for a consumer to list in its tsconfig `files`. A target naming none provides no entry |
| `transitive_global_entry_files` | `depset of File` | The closure of `global_entry_files`. A global is global to the whole program, so the closure travels, not the direct set |

`global_entry_files` is a file of references and not the declarations
themselves: Starlark cannot read a source to tell a global `.d.ts` from a module
one, so an action decides, and the provider names its answer at analysis time.
See [Which ambients a consumer gets](ts-compile.md#which-ambients-a-consumer-gets).

## TsModuleInfo

The bare specifier a target is importable as, and where its declarations land.
Only the producing target knows where its `.d.ts` files land under the current
configuration, so the name travels with the target and a consumer writes its
own `paths` entry from it.

| Field | Type | Description |
|---|---|---|
| `module_name` | `string` | The bare specifier, or `""` when the target declared none |
| `label` | `string` | The target's label, as a dep list writes it |
| `declaration_root` | `string` | Exec-root-relative directory the generated `.d.ts` files land in |
| `source_root` | `string` | Exec-root-relative package directory, where a `.d.ts` passed straight through stays |
| `declared_paths` | `tuple of struct(specifier, declarations)` | What the module's own `package.json` says its specifiers resolve to: `specifier` is the part after the module name (`""`, `"/button"`, `"/tokens/*"`), `declarations` the module-root-relative declaration paths, in resolution order. Empty on a target that declared its name with `module_name` |
| `transitive_modules` | `depset of struct` | This target's modules and its deps', each with the five fields above |

`ts_dev_server` reads the same provider to write one `resolve.alias` entry per
first-party `module_name`.

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

## DevServerInfo

`DevServerInfo` is not exported from `@rules_typescript//ts:defs.bzl`. It loads
from `@rules_typescript//ts/private:providers.bzl`, and everything under
`ts/private/` is [volatile](../compatibility.md#volatile). The shipped
implementation, `//vite:dev_server`, returns it; `ts_dev_server(server = ...)`
takes any target that does.

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

`//tests/toolchain:foreign_target_platform_test` pins the split. It analyses a
probe rule under `--platforms=//platforms:windows_amd64`, a platform with a Node
runtime and no compiler binary: oxc and tsgo still resolve to exec-platform
binaries, the staged runtime is `nodejs_windows_amd64`, and the tool runtime is
not.
