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

## Type Errors Not Surfacing

Type-checking runs only when a tsgo toolchain is registered and `enable_check = True` (both are defaults). The recommended way to enable it permanently is:

```
# .bazelrc
build --output_groups=+_validation
```

To trigger it for a single build without modifying `.bazelrc`:

```bash
bazel build //... --output_groups=+_validation
```

## tsgo Not Found

The tsgo toolchain is registered automatically by `rules_typescript`. If it is not resolving, confirm that your `bazel_dep` for `rules_typescript` is present and that no explicit `register_toolchains` call in your workspace is shadowing the defaults.

To use a specific tsgo version:

```python
ts = use_extension("@rules_typescript//ts:extensions.bzl", "ts")
ts.tsgo(version = "7.0.0-dev.20260311.1")
```

## Import Not Resolving in tsgo

tsgo uses `moduleResolution: "Bundler"` with `paths` entries for direct npm deps. If tsgo cannot resolve a bare import like `import { z } from "zod"`, add the package as a direct dep:

```python
ts_compile(
    name = "app",
    srcs = ["app.ts"],
    deps = ["@npm//:zod"],  # must be here, not just a transitive dep
)
```

## vitest Not Found at Test Runtime

The `node_modules` target must include vitest:

```python
node_modules(
    name = "node_modules",
    deps = ["@npm//:vitest"],
)

ts_test(
    name = "my_test",
    node_modules = ":node_modules",
    ...
)
```

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
2. For path aliases, verify `gazelle_ts.json` has the correct `pathAliases` entries.
3. Use `# gazelle:ts_ignore` to suppress generation for a directory and write its BUILD file manually.

## Slow First Build

The first build downloads: the Rust toolchain (for oxc_cli), tsgo npm tarballs, Node.js tarballs, and all npm packages from the lockfile. Subsequent builds are fully cached.

To speed up CI, mount a persistent Bazel cache volume:

```bash
docker run -v bazel-cache:/root/.cache/bazel my-image bazel build //...
```

## Container Builds

Bazel works correctly inside Docker containers without privileged mode:

```dockerfile
FROM ubuntu:24.04

RUN apt-get update && apt-get install -y curl git python3 \
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
- The Rust toolchain for `oxc-bazel` is the largest download (~500 MB) on the first build.
- ARM64 containers are supported — `rules_rust` builds `oxc-bazel` natively and `@typescript/native-preview-linux-arm64` provides tsgo.
