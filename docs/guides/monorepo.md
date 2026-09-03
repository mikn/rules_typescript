# Monorepo Layout

`bazel run //:gazelle` infers the targets from the tree. Every directory holding
`.ts` files becomes a `ts_compile` target, every test file a `ts_test`:

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

Gazelle's default is `every-dir`: every directory holding sources is a package,
as every directory with `.go` files is a Go package. Two directives depart from
it:
`# gazelle:ts_package_boundary tsconfig` for one target per TypeScript project,
and `# gazelle:ts_target_name` to rename one target.

Write a target by hand when a directory is a unit: a public API behind an
`index.ts`, something other packages import, something published as its own npm
package.

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

If `lib/math.ts` changes but its exported types don't change, `app` is not
recompiled. Bazel's content-based caching uses the `.d.ts` fingerprint as the
dependency boundary.

Every import has to be satisfied by a direct dep. A `.d.ts` that reaches a
target through another dep's own deps does not count, so the `deps` list above
is what `apps/server` may import; `bazel run //:gazelle` keeps it current. See
[Deps have to be direct](../rules/ts-compile.md#deps-have-to-be-direct).

Relative imports across packages work as written. For a bare specifier,
`import { Button } from "@acme/ui"`, set `module_name = "@acme/ui"` on the
producing target; the dependent then gets a `paths` entry pointing at whatever
`.d.ts` Bazel produced for it. Do not hand-write that entry, and do not point
`path_aliases` into `bazel-out/`: both break under a different configuration, and
the second is a hard error. See
[ts_compile](../rules/ts-compile.md#importing-another-target-by-bare-specifier).

## Single pnpm Lockfile

One `pnpm-lock.yaml` at the repo root covers all packages, with one
`npm.translate_lock` call for it. The extension declares one Bazel repository
per package and Bazel fetches only the ones a target reaches, so a 2731-entry
lockfile does not slow a one-package build.

pnpm workspaces work: a `workspace:*` dependency resolves to a target in your own
repository.

A second hub keeps a closure out of the tree an app's tests resolve against, or
keeps a curated fixture lockfile out of `pnpm add`'s reach. It costs two
lockfiles to keep in step, a Gazelle directive per package, and one
`ts_add_package` target per hub. See
[More than one hub](npm.md#more-than-one-hub).

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
