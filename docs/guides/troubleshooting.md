# Troubleshooting

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

## Isolated Declarations Error: Missing Return Type

When `isolated_declarations = True`, all exported functions and variables must have explicit type annotations. The tsgo type-checker will report:

```
error TS9007: Declaration emit for this file requires type resolution. ...
```

Add the missing return type or explicit type annotation to the export. See [Isolated Declarations](../getting-started/isolated-declarations.md) for the full migration guide.

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
