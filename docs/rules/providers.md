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
carries the files a consumer stages, its npm closure and the store files it
reaches, and `deps` on `ts_compile` and `ts_test` accepts any target returning
it.

A direct field carries only what the target itself produces. A rule that
forwards a dep's files leaves the direct field empty and puts the closure in the
transitive one; `ts_compile` does that for its deps' data files, which reach
`transitive_data` and never `data`. A consumer that wants everything reachable
reads the transitive field. Declarations have no transitive field: the
closure's are the `owners` records', one per target, so a consumer reads them
all or leaves a target's out.

| Field | Type | Description |
|---|---|---|
| `js` | `depset of File` | The `.js` files this target produces: compiled output, plus any JavaScript src staged as-is |
| `js_maps` | `depset of File` | The `.js.map` beside them |
| `declarations` | `depset of File` | The declarations this target produces, plus the ambient ones it passes through from `srcs`. Emitted when a consumer's compile reads them or `--output_groups=declarations` asks, never as a default output ([Which Tool Emits the Declarations](ts-compile.md#which-tool-emits-the-declarations)). A global one is in scope in a consumer only when the consumer's tsconfig `types` names it |
| `data` | `depset of File` | The srcs that are neither TypeScript, JavaScript nor declarations, staged at their package-relative paths beside the compiled `.js` |
| `manifest` | `File or None` | The `package.json` at the package's root as built, every source-file target rewritten to the emitted file, `<name>.package.json`; a dependent's program root lays it at the package's path and the member's store tree copies it there. The src as written is in `data` |
| `sources` | `depset of File` | The srcs the program reads as its own: `.ts`, `.tsx`, JavaScript and declarations. A `ts_test` in the same package stages them in its runfiles at their source paths; one under the same `tsconfig` checks them as its own program's ([The Test's Program](ts-test.md#the-tests-program)) |
| `tsconfig` | `File or None` | The `tsconfig.json` the program's options come from, the `tsconfig` attribute's file: the identity a `ts_test` joins a dep's sources by |
| `transitive_js` | `depset of File` | Every `.js` from this target and its first-party deps |
| `transitive_js_maps` | `depset of File` | Their `.js.map` |
| `transitive_data` | `depset of File` | The data files of this target and its first-party deps: what a compiled module reaches beside itself at run time or in a bundle |
| `transitive_es_twins` | `depset of (File, File)` | For a program tsgo emits, each `.js` of this target and its first-party deps paired with the ES module oxc emits from the same source; the vitest runner stages the second at the first's runfiles path ([The Module Format](ts-compile.md#the-module-format)) |
| `npm_packages` | `depset of NpmPackageInfo` | The npm closure of this target's deps: what the ownership manifest names and a runner checks its packages against. A package itself arrives through its `NpmPackageInfo` |
| `npm_files` | `depset of File` | The store files this target's program and runtime reach: the importer links of its direct npm deps and their `@types` twins, the member links its deps name, every store tree and edge link of their closures, the hoist links whose names the closure holds with the trees they enter ([The Store](node-modules.md#the-store)), and its first-party deps' `npm_files`. An action stages this and nothing else of the store. A dep's emitted `.d.ts` imports the packages the dep declared and resolves them from the dep's own importer's links, which this depset carries into the consumer's action |
| `owners` | `depset of struct(label, files, declarations)` | One record per first-party target in the closure, this one first: `label`, the string a `deps` list writes for it; `files`, the sources, declarations, data and manifest as built it stages; `declarations`, its own alone. The tsgo action reads the closure's records to name the target a listed file belongs to ([Deps have to be direct](ts-compile.md#deps-have-to-be-direct)); a consumer's program reads the `declarations` of every record in its closure but a dep's it holds as sources, so that dep's declarations arrive by no path ([The Test's Program](ts-test.md#the-tests-program)). An npm package's declarations reach a consumer through `npm_files`, not through a record |

A dep reached through the store -- an `@npm` package, a member's link target
-- reaches the consumer's program and runtime there: `ts_compile` reads
`npm_packages` off it and none of its file fields, and stages its link and
store files, so an npm package's `TsInfo` stages nothing by path and its
`owners` is empty. A first-party dep's files are staged
at their exec paths, its `declarations`, `js` and `data` are what an import may
resolve to, and its `owners` record is what names it when an import does
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
| `packages` | `list of string` | The npm packages the runner needs in the test's npm closure, `vitest` for the vitest runner; `ts_test` fails at analysis naming the one no dep provides |
| `hook` | `File` | The one module the runner loads into node before the tests: the node:test runner's resolver, the vitest runner's reads recorder |
| `es_modules` | `bool` | `True` when the runner runs the program as ES modules whatever its tsconfig's `module` -- vitest -- so `ts_test` emits its srcs as such and stages a dep's ES twins; `False` for node:test, which runs the package's format ([Runners](ts-test.md#runners)) |
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
`npm_workspace_package`; a `node_modules` target links `store`'s tree under
`package_name`, `ts_compile` resolves a direct dep to that link, and `store`
names the snapshot's tree in [the store](node-modules.md#the-store).

| Field | Type | Description |
|---|---|---|
| `package_name` | `string` | The npm name, `react` or `@types/react`; what the tree links the package as |
| `package_version` | `string` | The version; `0.0.0` on a workspace member, which pnpm resolves by path |
| `peer_id` | `string` | A filesystem-safe token naming the peer set this resolution was made against, empty for a package pnpm resolved only one way. Two snapshots can share `name@version` and differ only here |
| `package_dir` | `File or None` | The `package.json` at the root of the extracted package. `None` on a workspace member, whose compile writes the manifest as built |
| `package_root` | `string` | Exec-root-relative directory the files in `all_files` hang off: where `package_dir` sits for an extracted tarball, the member's directory under `bazel-bin` for a workspace member |
| `all_files` | `depset of File` | Every file of the package (`package.json`, `.js`, `.d.ts`, other assets), the files its store tree copies; a member's are its outputs, the manifest as built in place of the src |
| `transitive_deps` | `depset of NpmPackageInfo` | Every npm package reachable from this one, the paired `@types/*` package included |
| `store` | `NpmStoreInfo` | The snapshot's store tree and the links beside it: `key`, `tree`, `links`, `transitive` (`npm/private/store.bzl`) |

The package's `exports`, `types` and `main` are nowhere in it: tsgo and node
read the manifest in the tree, as they do over an install.

## DevServerInfo

The shipped implementation, `//vite:dev_server`, returns it;
`ts_dev_server(server = ...)` takes any target that does.

| Field | Type | Vite | Description |
|---|---|---|---|
| `server_binary` | `File or None` | `None` | The server executable, for a server that is a build artifact. `None` when it ships inside the npm tree |
| `server_in_tree` | `string` | `"vite/bin/vite.js"` | The executable's path under the importer's `node_modules` directory, for a server that ships as an npm package. Exactly one of the two is set |
| `argv` | `list of string` | `["dev", "--config", "{config}"]` | The command line after the executable. `{config}` expands to the generated config's path, `{port}` to the `port` attr, `{root}` to the directory served |
| `config_dialect` | `string` | `"vite"` | The config format the server is handed. Only `"vite"` is generated today; a server reading its own format declares its own dialect, and the generator has to learn it before that server can be selected |
| `runs_in_js_runtime` | `bool` | `True` | `True` when the executable is JavaScript and the toolchain Node runs it. A native server still gets the toolchain Node on `PATH`, for a plugin host that is a Node process |
| `ignored_config_fields` | `list of string` | `[]` | Dotted config paths the server does not honour. A target whose configuration reaches one fails at analysis time naming the field and the server |
| `native_react_refresh` | `bool` | `False` | `True` when the server applies React Fast Refresh itself. `react_refresh = True` then fails at analysis time |
| `runtime_deps` | `depset of File` | empty | Everything the server needs in runfiles beyond the generated config and the importer's `node_modules` |

A server shipping as an npm package has no `File` to point at: its executable is
a path under the importer's `node_modules` directory, reached through the
package's link, which no artifact names. That is why `server_in_tree` exists
beside `server_binary`.

## NodeModulesInfo

A [`node_modules`](node-modules.md) target returns it; `ts_codegen`,
`ts_binary`, `ts_dev_server` and `esbuild_bundle` read it from their
`node_modules` attr, and take the target's `DefaultInfo.files` -- every link
and every store tree the links reach -- as inputs or runfiles; `ts_compile`
and `ts_test` follow `parent` up the chain and stage the links a direct dep
resolves to ([The Chain](node-modules.md#the-chain)), and from `hoist`, the
lockfile's hidden hoist carried unchanged down the chain, the links whose
names the closure holds ([The Store](node-modules.md#the-store)).

| Field | Type | Description |
|---|---|---|
| `label` | `Label` | The `node_modules` target's: the importer's package, and what a message names |
| `dir` | `string` | The importer's `node_modules` directory as a bin-dir path, `bazel-out/<cfg>/bin/<package>/node_modules`: the parent of every link, which no artifact names |
| `links` | `dict of string -> NpmLinkInfo` | Per package name, the declared symlink `node_modules/<name>` and the store it enters |
| `parent` | `NodeModulesInfo or None` | The importer above's |
| `hoist` | `NpmHoistInfo` | The lockfile's hidden hoist, the root importer's `hoist` target's, the same on every importer of its chain ([NpmHoistInfo](#npmhoistinfo)) |

## NpmHoistInfo

A lockfile's hidden hoist, what its `npm_store_hoist` target
`node_modules/.pnpm/node_modules` returns
([The Store](node-modules.md#the-store)); the root importer names the target
in `hoist`, and every importer on the chain carries it as
`NodeModulesInfo.hoist`.

| Field | Type | Description |
|---|---|---|
| `links` | `dict of string -> NpmLinkInfo` | Per hoisted name, the declared symlink `node_modules/.pnpm/node_modules/<name>` (a `public-hoist-pattern` match's at the root importer's `node_modules/<name>`) and the store it enters |
| `members` | `dict of string -> File` | Per hoisted workspace member, the declared symlink alone, with no dependency on the member's tree; a consumer's closure holds the tree through the member's link target |

## NpmLinkInfo

One link `node_modules/<name>` into a store tree: an entry of
`NodeModulesInfo.links`, and what a [`node_modules_member`](node-modules.md)
target returns beside the member's `TsInfo` and `NpmPackageInfo`, so a
`ts_compile` or `ts_test` names the link target in `deps` where it named the
hub's view, and a `ts_codegen` names it there for a generator that resolves
the member.

| Field | Type | Description |
|---|---|---|
| `link` | `File` | The declared symlink `node_modules/<name>` |
| `store` | `NpmStoreInfo` | The store the link enters |

## Toolchain Contract

`@rules_typescript//ts/toolchain:defs.bzl` exports seventeen names: six
toolchain type labels, five providers, and six accessors.

```python
load(
    "@rules_typescript//ts/toolchain:defs.bzl",
    "JS_RUNTIME_TOOLCHAIN_TYPE",
    "JS_TOOL_TOOLCHAIN_TYPE",
    "LAUNCHER_TOOLCHAIN_TYPE",
    "OXC_TOOLCHAIN_TYPE",
    "TOOLS_TOOLCHAIN_TYPE",
    "TSGO_TOOLCHAIN_TYPE",
    "JsRuntimeInfo",
    "LauncherInfo",
    "OxcToolchainInfo",
    "ToolsInfo",
    "TsgoToolchainInfo",
    "get_js_runtime",
    "get_js_tool",
    "get_launcher_toolchain",
    "get_oxc_toolchain",
    "get_tools_toolchain",
    "get_tsgo_toolchain",
)
```

| Type label | Target | Runs on | Accessor | Returns |
|---|---|---|---|---|
| `OXC_TOOLCHAIN_TYPE` | `//ts/toolchain:oxc_toolchain_type` | the exec platform | `get_oxc_toolchain(ctx)` | `OxcToolchainInfo` |
| `TSGO_TOOLCHAIN_TYPE` | `//ts/toolchain:tsgo_toolchain_type` | the exec platform | `get_tsgo_toolchain(ctx)` | `TsgoToolchainInfo` |
| `TOOLS_TOOLCHAIN_TYPE` | `//ts/toolchain:tools_toolchain_type` | the exec platform | `get_tools_toolchain(ctx)` | `ToolsInfo` |
| `LAUNCHER_TOOLCHAIN_TYPE` | `//ts/toolchain:launcher_toolchain_type` | the target platform | `get_launcher_toolchain(ctx)` | `LauncherInfo`, or `None` when no toolchain resolved |
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
| `ToolsInfo` | `tsaction` | `File` | The runner behind the TsConfig, TsEmit, tsgo, TsLint, TsTestPaths, TsManifest and NpmStore actions |
| | `lcov_merger` | `File` | `ts_test`'s coverage merger, run through `//ts/toolchain:lcov_merger_resolved` |
| | `copy_to_workspace` | `File` | The copier `ts_refresh_tsconfig` writes the source tree with |
| `LauncherInfo` | `launcher` | `File` | The launcher a `ts_test`, `ts_binary`, `ts_dev_server` or `npm_bin` executable is a symlink of |
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

`get_oxc_toolchain`, `get_tsgo_toolchain` and `get_tools_toolchain` index
`ctx.toolchains` directly, so a rule listing any of the three types as mandatory
fails toolchain resolution when nothing registers one. `get_js_runtime`,
`get_js_tool` and `get_launcher_toolchain` return `None` for a type declared
with `mandatory = False` that nothing registered; `ts_compile` declares tsgo and
the JS tool that way, and oxc and the tools as mandatory; the four rules that
run a launcher declare it that way and fail analysis naming the target platform
when none resolved.

`register_toolchains("@rules_typescript//ts/toolchain:all")` registers every
instance: one oxc toolchain, built from source by rules_rust for whichever exec
platform runs the build; one tsgo toolchain per platform in `TSGO_PLATFORMS`,
constrained on the exec platform; one tools toolchain and one launcher
toolchain per platform in `TSGO_PLATFORMS` over the binaries of the tools
release `ts/private/tools_lock.bzl` names, the tools constrained on the exec
platform and the launcher on the target platform; one Node runtime toolchain
per platform in `NODE_PLATFORMS`, constrained on the target platform; and one
Node tool toolchain per platform, constrained on the exec platform.

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

### tsgo from source

`//ts/toolchain/tsgo_source` is the same compiler built by rules_go.
`ts/private/tsgo_source/go.mod` names
`github.com/microsoft/typescript-go/cmd/tsgo` in a `tool` directive and
requires the module at the commit the lockfile's `typescript` release was
built from, so the two toolchains run one compiler at one revision, and a
change the ruleset makes to that source reaches a build that registers this
one. The `ts` extension reads the go.mod and its go.sum and declares one
Gazelle-written `go_repository` per require, the module's dependencies under
the names Gazelle writes for them; `@tsgo_source//:tsgo` aliases the module's
`cmd/tsgo` binary, and the toolchain over it constrains no platform, since
`cfg = "exec"` on the binary builds it for whichever platform runs the build.
The lockfile's binary reads its `lib/*.d.ts` from beside itself
(`-tags=noembed`); this build embeds them, so the binary is one file. The
toolchain is outside `//ts/toolchain:all`, so a consumer registers it by name,
first:

```python
register_toolchains(
    "@rules_typescript//ts/toolchain/tsgo_source",
    "@rules_typescript//ts/toolchain:all",
)
```

The pin moves in `ts/private/tsgo_source` with
`go get github.com/microsoft/typescript-go@<commit> && go mod tidy`; for
TypeScript 7.1 the Go code moved into `github.com/microsoft/TypeScript` under
`tsc/`, so the next pin changes the module path in the `tool` line and the
require. `//ts/private/tsgo:tsgo_test` reads the revision the lockfile's
linux-amd64 binary embeds (`go version -m` prints it as `vcs.revision`) and
fails when it is not the pinned one, naming both, so a `typescript` bump in
the lockfile names the pin to move. A build that first resolves the toolchain
compiles the module: 91 actions, 113.5 s wall and an 89.6 s critical path on
a 22-core machine at load 11, cached after. rules_go's apparent name in this
module is `io_bazel_rules_go`, the name Gazelle writes into the BUILD files
of a repository with no MODULE.bazel of its own.

`//tests/toolchain:foreign_target_platform_test` pins the split. It analyses a
probe rule under `--platforms=//platforms:windows_amd64`, a platform with a Node
runtime and no compiler binary: oxc and tsgo still resolve to exec-platform
binaries, the staged runtime is `nodejs_windows_amd64`, and the tool runtime is
not.
