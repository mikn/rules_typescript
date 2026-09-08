# Monorepo Layout

`bazel run //:gazelle` writes one package per `tsconfig.json`: the directory
holding the file gets a `ts_compile` over what the program lists, a `ts_test`
over its test files, and a `ts_config` over the file:

```
my-monorepo/
├── MODULE.bazel
├── pnpm-lock.yaml          # single lockfile for all packages
├── tsconfig.json           # the base every package extends; //:tsconfig
├── packages/
│   ├── ui/
│   │   ├── BUILD.bazel     # ts_compile(name = "ui", ...), ts_config
│   │   ├── package.json
│   │   ├── tsconfig.json
│   │   └── src/index.ts
│   └── utils/
│       ├── BUILD.bazel     # ts_compile(name = "utils", ...), ts_config
│       ├── package.json
│       ├── tsconfig.json
│       └── src/index.ts
└── apps/
    └── server/
        ├── BUILD.bazel     # ts_compile that depends on @npm//:ui, @npm//:utils
        ├── package.json
        ├── tsconfig.json
        └── src/main.ts
```

## Package Boundaries

A package is a TypeScript project: the unit `tsc -p` compiles is the unit
Bazel builds, and the tsconfig's `include`, `files` and `exclude` say what it
holds. A directory under a package with no `tsconfig.json` of its own is part
of the package above; a directory with one is a package of its own, and the
parent's edge into it is a dep. See
[the package model](../gazelle/overview.md#the-package-model).

```python
# packages/utils/BUILD.bazel
load("@rules_typescript//ts:defs.bzl", "ts_compile", "ts_config")

ts_compile(
    name = "utils",
    srcs = [
        "package.json",
        "src/index.ts",
        "src/number.ts",
        "src/string.ts",
    ],
    tsconfig = ":tsconfig",
    visibility = ["//visibility:public"],
)

ts_config(
    name = "tsconfig",
    src = "tsconfig.json",
    visibility = ["//visibility:public"],
    deps = ["//:tsconfig"],
)
```

```python
# apps/server/BUILD.bazel
load("@rules_typescript//ts:defs.bzl", "ts_compile", "ts_config")

ts_compile(
    name = "server",
    srcs = [
        "package.json",
        "src/main.ts",
    ],
    tsconfig = ":tsconfig",
    visibility = ["//visibility:public"],
    deps = [
        "@npm//:ui",
        "@npm//:utils",
        "@npm//apps/server:express",
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

If `lib/math.ts` changes but its exported types don't change, `app` is not
recompiled. Bazel's content-based caching uses the `.d.ts` fingerprint as the
dependency boundary.

Every import has to be satisfied by a direct dep. A `.d.ts` that reaches a
target through another dep's own deps does not count, so the `deps` list above
is what `apps/server` may import; `bazel run //:gazelle` keeps it current from
tsgo's listing of each program. See
[Deps have to be direct](../rules/ts-compile.md#deps-have-to-be-direct).

A relative import into another package resolves to that package's
`ts_compile`. A bare specifier, `import { Button } from "@acme/ui"`, names a
workspace member, which the dependent reaches through the hub's view of it,
`@npm//:acme_ui`, as it reaches any npm package; see
[importing another target by bare specifier](../rules/ts-compile.md#importing-another-target-by-bare-specifier).

## Single pnpm Lockfile

One `pnpm-lock.yaml` at the repo root covers all packages, with one
`npm.translate_lock` call for it. The extension declares one Bazel repository
per package and Bazel fetches only the ones a target reaches, so a 2731-entry
lockfile does not slow a one-package build.

pnpm workspaces work: a `workspace:*` dependency resolves to a target in your own
repository.

A second hub keeps a closure out of the tree an app's tests resolve against, or
keeps a curated fixture lockfile out of `pnpm add`'s reach. It costs two
lockfiles to keep in step, one `ts_add_package` target per hub, and
hand-written `deps` under `# keep` for the packages on it, since Gazelle
writes `@npm` alone. See [More than one hub](npm.md#more-than-one-hub).

## Visibility

Set `visibility = ["//visibility:public"]` on packages that other packages depend
on. Keep leaf-node packages at `["//visibility:private"]` unless needed
externally. Gazelle writes `//visibility:public` on every `ts_compile` target it
generates.

`ts_refresh_tsconfig`'s `deps` is a normal rule attribute, so it obeys visibility
too. A package-private target cannot be listed there, and the IDE's
`tsconfig.json` carries no `paths` entry for it, because the aspect never reaches
it. A hand-written target the editor should see needs a visibility grant to the
root package. See
[IDE Setup](../getting-started/ide-setup.md#setup).

The `ts_compile` targets `ts_test` generates from your sources take the test's
`visibility`, and are public when it declares none. An IDE tsconfig can
therefore see the npm packages only a test declares.
