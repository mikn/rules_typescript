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

Gazelle's default is **every-dir**: every directory holding `.ts` files gets its
own `ts_compile` target, the way every directory with `.go` files is a Go
package. You do not choose boundaries — you choose whether to depart from that,
with `# gazelle:ts_package_boundary index-only` for the older
`index.ts`-marks-a-package behaviour, or `# gazelle:ts_target_name` to rename
one.

Hand-written targets are worth it when a directory is a genuine unit: a public
API behind an `index.ts`, something other packages import, something published
as its own npm package.

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

Relative imports across packages work as written. For a bare specifier —
`import { Button } from "@acme/ui"` — set `module_name = "@acme/ui"` on the
producing target; the dependent then gets a `paths` entry pointing at whatever
`.d.ts` Bazel produced for it. Do not hand-write that entry, and do not point
`path_aliases` into `bazel-out/`: both break under a different configuration,
and the second is now a hard error. See
[ts_compile](../rules/ts-compile.md#importing-another-target-by-bare-specifier).

## Using Gazelle

Run Gazelle once to generate BUILD files for the entire monorepo:

```bash
bazel run //:gazelle
```

Gazelle creates `ts_compile` targets for every directory with TypeScript files, resolves import paths to Bazel labels, and generates `ts_test` targets for test files. After adding new source files or packages, re-run Gazelle to update BUILD files.

## Single pnpm Lockfile

Use a single `pnpm-lock.yaml` at the repo root covering all packages, and one
`npm.translate_lock` call for it. That avoids the version conflicts per-package
lockfiles create, and it costs nothing in fetch time: the extension declares one
Bazel repository per package and Bazel fetches only the ones your targets
actually reach. A 2731-entry lockfile does not make a one-package build slow.

pnpm workspaces work: a `workspace:*` dependency resolves to the target in your
own repository rather than to a download.

## Visibility

Set `visibility = ["//visibility:public"]` on packages that other packages depend
on. Keep leaf-node packages at `["//visibility:private"]` unless needed
externally. Gazelle takes the other line and writes `//visibility:public` on every
`ts_compile` target it generates.

That choice has one non-obvious consequence: `ts_refresh_tsconfig`'s `deps` is a
normal rule attribute, so it obeys visibility too. A package-private target
cannot be listed there, and the IDE's `tsconfig.json` will not carry a `paths`
entry for it — the aspect never reaches it. In a hand-written monorepo, the
targets you want your editor to see need a visibility grant to the root package.
See [IDE Setup](../getting-started/ide-setup.md#setup).
