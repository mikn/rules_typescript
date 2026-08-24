# Troubleshooting

## No repository visible as '@rules_rust'

```
ERROR: @rules_rust//rust/toolchain/channel :: Error loading option
@rules_rust//rust/toolchain/channel: No repository visible as '@rules_rust'
from main repository
```

A `.bazelrc` in your workspace sets a `--@rules_rust//...` flag. Delete the
line. `rules_rust` is a transitive dependency of `rules_typescript`, so
`@rules_rust` is not in your module's repo mapping and Bazel cannot resolve
the flag's label. Nothing in a consumer's `.bazelrc` needs it: the Rust
toolchain channel already defaults to `stable`, which is the only channel
`rules_typescript` registers a toolchain for.

If you really do need `@rules_rust` flags for Rust code of your own, add
`bazel_dep(name = "rules_rust", version = "0.69.0")` to your `MODULE.bazel` to
bring the repo into your mapping — but note that the flag then applies to the
`oxc-bazel` build too, and a non-`stable` channel will fail toolchain
resolution.

## BUILD file not found for //:MODULE.bazel

```
Error in path: Unable to load package for //:MODULE.bazel: BUILD file not
found in any of the following directories.
```

`rules_rust`'s crate fetching resolves `//:MODULE.bazel`, which requires your
repository root to be a Bazel package. Create a `BUILD.bazel` at the root; an
empty file is enough, though the [quickstart](../getting-started/quickstart.md)
puts the Gazelle target there.

## No tsgo toolchain / declarations are missing

Toolchain registration is the **consumer's** job. `rules_typescript` deliberately
registers nothing on your behalf, the same way `rules_go` does not. Your
`MODULE.bazel` needs:

```python
register_toolchains("@rules_typescript//ts/toolchain:all")
```

Without it, nothing resolves the tsgo toolchain, and under the default
`declarations = "tsgo"` that means no `.d.ts` and no type-checking.

To pin a tsgo version:

```python
ts = use_extension("@rules_typescript//ts:extensions.bzl", "ts")
ts.tsgo(version = "7.0.0-dev.20260311.1")
```

On Windows there is no tsgo binary to resolve at all — see
[COMPATIBILITY.md](https://github.com/mikn/rules_typescript/blob/main/COMPATIBILITY.md#windows).

## compilerOptions.X is set by the rule and cannot be overridden

```
ts_compile: compilerOptions.paths is set by the rule and cannot be overridden --
use path_aliases for source aliases, or module_name on the target that produces
the declarations.
Remove "paths" from compiler_options on //src/app:app.
```

Fifteen `compilerOptions` keys encode the sandbox layout or the action's
declared outputs, so `compiler_options` refuses them rather than applying a
value that would break the build. The message names the attribute to use
instead; the full list is in
[ts_compile](../rules/ts-compile.md#the-two-hard-errors).

## path_aliases points into the output tree

```
ts_compile: path_aliases["@acme/ui"] on //src/app:app points into the output
tree (bazel-out/k8-fastbuild/bin/packages/ui).
```

A path under `bazel-out/` embeds the build configuration, so it stops resolving
under `-c opt` or a different exec platform. `path_aliases` is for **source**
directories. To import another target by bare specifier, set `module_name` on
the target that produces its declarations and depend on it.

## npm: pnpm-lock.yaml declares patchedDependencies with no patch file

Your lockfile patches a package and the extension was not given the patch. Pass
it as a label:

```python
npm.translate_lock(
    pnpm_lock = "//:pnpm-lock.yaml",
    patches = ["//patches:@pierre__diffs@1.3.1.patch"],
)
```

The reverse — a patch file no `patchedDependencies` entry claims — fails too,
and means the lockfile is stale or the file is misnamed. Names follow
`pnpm patch-commit`: `<name with / replaced by __>@<version>.patch`. See
[npm Dependencies](npm.md#patched-dependencies).

## Import Not Resolving in tsgo

tsgo uses `moduleResolution: "Bundler"` with `paths` entries for direct npm deps. If tsgo cannot resolve a bare import like `import { z } from "zod"`, add the package as a direct dep:

```python
ts_compile(
    name = "app",
    srcs = ["app.ts"],
    deps = ["@npm//:zod"],  # must be here, not just a transitive dep
)
```

## ts_test: vitest not found

```
ts_test: vitest not found. Set vitest attr or include it in node_modules.
```

`vitest` has to be reachable from the tree the test runs against. With the
default (auto) `node_modules`, that means listing it in `deps`:

```python
ts_test(
    name = "my_test",
    srcs = ["my.test.ts"],
    deps = [":my_lib", "@npm//:vitest"],
)
```

With an explicit `node_modules` target, put it there instead — the auto
generation is skipped entirely when `node_modules` is set:

```python
node_modules(
    name = "node_modules",
    deps = ["@npm//:vitest"],
)

ts_test(
    name = "my_test",
    srcs = ["my.test.ts"],
    deps = [":my_lib"],
    node_modules = ":node_modules",
)
```

The same applies to every package the run needs at runtime, including ones only
the production code imports: the auto tree is built from `ts_test`'s direct npm
deps and their transitive npm deps, and a `ts_compile` dep does not contribute
its own.

## Isolated declarations error: missing return type

Only reachable under `declarations = "oxc"`, where Oxc derives `.d.ts` from
syntax and so needs an explicit type on every export:

```
× Isolated declarations error(s): TS9013: Expression type can't be inferred
│ with --isolatedDeclarations.
```

Two ways out. Add the annotation Oxc names, or drop that target back to the
default `declarations = "tsgo"`, where the compiler infers it. See
[Isolated Declarations](../getting-started/isolated-declarations.md).

## Type errors are not failing the build

Under the default `declarations = "tsgo"` they always do — the `.d.ts` are real
outputs of the type-checking action, so a target with a type error produces
nothing.

If a target sets `declarations = "oxc"`, type-checking is a validation action
instead, and Bazel only runs those when asked:

```
build --output_groups=+_validation
```

If a target sets `enable_check = False`, nothing type-checks it at all. Under
`"oxc"` that still gives you complete declarations (Oxc enforces isolated
declarations itself); under `"tsgo"` it means the target emits no `.d.ts`,
which is intended for terminal targets whose declarations nothing consumes.

## Gazelle Generating Wrong Deps

If Gazelle generates incorrect `deps` for an import:

1. Check that the import specifier matches an npm package name in the lockfile.
2. For path aliases, check `compilerOptions.paths` in the nearest
   `tsconfig.json` — Gazelle reads it directly, as JSONC — or set
   `# gazelle:ts_path_alias @/ src/` explicitly.
3. Use `# gazelle:ts_ignore` to suppress generation for a directory and write
   its BUILD file manually.

## Slow First Build

The first build downloads a Rust toolchain (and compiles `oxc_cli` from
source), a tsgo npm tarball, a Node.js tarball, and the npm packages your
targets actually reach — not the whole lockfile. Subsequent builds are cached.

To speed up CI, mount a persistent Bazel cache volume:

```bash
docker run -v bazel-cache:/root/.cache/bazel my-image bazel build //...
```

## Container Builds

Bazel works correctly inside Docker containers without privileged mode:

```dockerfile
FROM ubuntu:24.04

RUN apt-get update && apt-get install -y curl git \
    && rm -rf /var/lib/apt/lists/*

RUN curl -Lo /usr/local/bin/bazel \
    https://github.com/bazelbuild/bazelisk/releases/latest/download/bazelisk-linux-amd64 \
    && chmod +x /usr/local/bin/bazel

WORKDIR /workspace
COPY . .
RUN bazel build //...
```

Key points:
- Mount a cache volume to avoid re-downloading toolchains on each run.
- The first build's long pole is Rust: `rules_rust` fetches a toolchain and
  then compiles `oxc-bazel` and its crate graph from source. Budget minutes, not
  seconds, and cache it.
- ARM64 containers are supported — `rules_rust` builds `oxc-bazel` natively and `@typescript/native-preview-linux-arm64` provides tsgo.
