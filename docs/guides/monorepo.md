# Monorepo Layout

`rules_typescript` is designed for monorepos. The recommended layout:

```
my-monorepo/
├── MODULE.bazel
├── pnpm-lock.yaml          # single lockfile for all packages
├── packages/
│   ├── ui/
│   │   ├── BUILD.bazel     # ts_compile(name = "ui", ...)
│   │   └── index.ts
│   ├── utils/
│   │   ├── BUILD.bazel     # ts_compile(name = "utils", ...)
│   │   └── index.ts
│   └── config/
│       ├── BUILD.bazel
│       └── index.ts
└── apps/
    └── server/
        ├── BUILD.bazel     # ts_compile that depends on //packages/ui, //packages/utils
        └── main.ts
```

## Package Boundaries

A directory should have its own `ts_compile` target when:

1. It has an `index.ts` that forms a public API (Gazelle auto-detects this).
2. Other packages import from it — cross-package imports must go through the `ts_compile` target.
3. It will be published as a separate npm package.

```python
# packages/utils/BUILD.bazel
load("@rules_typescript//ts:defs.bzl", "ts_compile")

ts_compile(
    name = "utils",
    srcs = ["index.ts", "string.ts", "number.ts"],
    visibility = ["//visibility:public"],  # allow other packages to depend on this
)
```

```python
# apps/server/BUILD.bazel
load("@rules_typescript//ts:defs.bzl", "ts_compile")

ts_compile(
    name = "server",
    srcs = ["main.ts"],
    deps = [
        "//packages/utils",
        "//packages/ui",
        "@npm//:express",
    ],
)
```

## Cross-Package Dependencies

`.d.ts` files are the compilation boundary between packages:

```python
# //lib/BUILD.bazel
ts_compile(
    name = "lib",
    srcs = ["math.ts"],
    visibility = ["//visibility:public"],
)

# //app/BUILD.bazel
ts_compile(
    name = "app",
    srcs = ["main.ts"],
    deps = ["//lib"],
)
```

If `lib/math.ts` changes but its exported types don't change, `app` is not recompiled. Bazel's content-based caching uses the `.d.ts` fingerprint as the dependency boundary.

## Using Gazelle

Run Gazelle once to generate BUILD files for the entire monorepo:

```bash
bazel run //:gazelle
```

Gazelle creates `ts_compile` targets for every directory with TypeScript files, resolves import paths to Bazel labels, and generates `ts_test` targets for test files. After adding new source files or packages, re-run Gazelle to update BUILD files.

## Single pnpm Lockfile

Use a single `pnpm-lock.yaml` at the repo root covering all packages. The `npm_translate_lock` extension reads this one file and creates the `@npm` repository that all packages share. This is simpler than per-package lockfiles and avoids version conflicts.

## Visibility

Set `visibility = ["//visibility:public"]` on packages that other workspaces depend on. Keep leaf-node packages at `["//visibility:private"]` unless needed externally.
