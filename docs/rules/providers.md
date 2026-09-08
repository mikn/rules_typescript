# Providers and Toolchains

The contract a rule outside this ruleset writes against: the providers the
rules return, and the toolchains they resolve. Four providers load from
`@rules_typescript//ts:defs.bzl`; the toolchain contract loads from
`@rules_typescript//ts/toolchain:defs.bzl`.

```python
load(
    "@rules_typescript//ts:defs.bzl",
    "BundlerInfo",
    "DevServerInfo",
    "TsInfo",
    "TsTestRunnerInfo",
)
```

| Provider | Returned by |
|---|---|
| `TsInfo` | `ts_compile`, `ts_codegen`, `ts_binary`, the `@npm` package targets and a member's hub view |
| `TsTestRunnerInfo` | `//ts/runners:vitest`, `//ts/runners:node_test`, a runner of your own |
| `BundlerInfo` | any rule that [brings its own bundler](../guides/bundling.md#custom-bundler-bundlerinfo-interface); the ruleset ships none |
| `DevServerInfo` | `//vite:dev_server`, a [server of your own](../guides/dev-server.md#bringing-your-own-server) |

## TsInfo

A dep provides one thing: what a consumer's program and runtime need. `TsInfo`
carries the files a consumer stages and the npm packages its forest links, and
`deps` on `ts_compile` and `ts_test` accepts any target returning it.

A direct field carries only what the target itself produces. A rule that
forwards a dep's files leaves the direct field empty and puts the closure in the
transitive one; `ts_compile` does that for its deps' data files, which reach
`transitive_data` and never `data`. A consumer that wants everything reachable
reads the transitive field.

| Field | Type | Description |
|---|---|---|
| `js` | `depset of File` | The `.js` files this target produces: compiled output, plus any JavaScript src staged as-is |
| `js_maps` | `depset of File` | The `.js.map` beside them |
| `declarations` | `depset of File` | The declarations this target produces, plus the ambient ones it passes through from `srcs`. A global one is in scope in a consumer only when the consumer's tsconfig `types` names it |
| `data` | `depset of File` | The srcs that are neither TypeScript, JavaScript nor declarations, staged at their package-relative paths beside the compiled `.js` |
| `sources` | `depset of File` | The TypeScript srcs, `.ts`, `.tsx` and declarations; a `ts_test` in the same package stages them in its runfiles at their source paths |
| `transitive_js` | `depset of File` | Every `.js` from this target and its first-party deps |
| `transitive_js_maps` | `depset of File` | Their `.js.map` |
| `transitive_declarations` | `depset of File` | Every declaration from this target and its first-party deps. An npm package's declarations reach a consumer through the node_modules forest its tsgo action stages, not through this depset |
| `transitive_data` | `depset of File` | The data files of this target and its first-party deps: what a compiled module reaches beside itself at run time or in a bundle |
| `npm_packages` | `depset of NpmPackageInfo` | The npm packages a consumer links into its forest and runtime tree for this target's deps. A dep's emitted `.d.ts` imports the packages the dep declared and resolves them in the consumer's program by walking that forest. A package itself arrives through its `NpmPackageInfo` |

A dep linked in the forest -- an `@npm` package, a member's hub view -- reaches
the consumer's program and runtime there: `ts_compile` reads `npm_packages`
off it and none of its file fields, so an npm package's `TsInfo` stages
nothing by path. A first-party dep's files are staged at their exec paths and
its `declarations`, `js` and `data` are what an import may resolve to
([Deps have to be direct](ts-compile.md#deps-have-to-be-direct)).

`ts_binary` with a `bundler` returns the bundle as the one member of both `.js`
fields, and the entry's data closure; without one it returns the entry's
`TsInfo`. `ts_codegen` with `out_dir` returns the directory as the one member
of `js` and `declarations`; nothing downstream compiles the tree, so what it
holds is already compiled output. An `outs` codegen returns its `.js` outs in
`js` and its `.d.ts` outs in `declarations`, so `deps = [":worker_types"]` is
legal and a consumer's tsconfig `types` can name the generated declaration.

A global `.d.ts` travels as a declaration output and nothing more: the consumer
names it in its own tsconfig `types` to bring its globals into scope. See
[Which ambients a consumer gets](ts-compile.md#which-ambients-a-consumer-gets).

## TsTestRunnerInfo

| Field | Type | Description |
|---|---|---|
| `packages` | `list of string` | The npm packages the runner needs in the test's `node_modules` tree, `vitest` for the vitest runner; `ts_test` fails at analysis naming the one no dep provides |
| `hook` | `File` | The one module the runner loads into node before the tests: the node:test runner's resolver, the vitest runner's reads recorder |
| `launch` | `function` | The runner's half of one test's analysis: given the test's `ctx` and the struct `ts_test` builds from the compile, it returns the launcher config's mode and section, the env, and the runfiles the runner adds |

A runner is a target, the way a toolchain is: `//ts/runners:vitest` and
`//ts/runners:node_test` are the two shipped, and a rule in another ruleset
returning this provider is a third. See [Runners](ts-test.md#runners).

## BundlerInfo

| Field | Type | Description |
|---|---|---|
| `bundler_binary` | `File` | The bundler executable |
| `config_file` | `File or None` | A static config passed as `--config`, in mode 1 only |
| `runtime_deps` | `depset of File` | Files the bundler needs at run time |
| `use_generated_config` | `bool` | `True` selects mode 2: `ts_binary` generates a `vite.config.mjs` and passes it with the entry, the output directory and the stylesheet. Default `False` |

The two invocation modes and the recipe for a bundler of your own are in
[Bundling](../guides/bundling.md#custom-bundler-bundlerinfo-interface).

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
