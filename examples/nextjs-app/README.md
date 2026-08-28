# examples/nextjs-app

A Next.js 15 application demonstrating the hybrid monorepo pattern with
rules_typescript: both routers, both API-route flavours, a server-rendered
route, and the two ways to run the app.

## What this demonstrates

- `next_build` rule wrapping `next build` as a hermetic Bazel action
- App Router pages and a route handler (`app/`), Pages Router pages and an API route (`pages/`)
- `/dynamic` and `/ssr`, server-rendered per request: both echo the `Host` the request carried
- `next_dev_server` (`next dev` over source) and `next_serve` (`next start` over the build)
- Shared TypeScript library (`packages/shared`) using `ts_compile` for incremental `.d.ts` boundary caching
- vitest tests for shared packages via `ts_test`
- Gazelle auto-generating `ts_compile` and `ts_test` BUILD rules from TypeScript sources
- `staging_srcs` staging shared library sources into the Next.js build directory so relative imports resolve without `transpilePackages`
- tsgo type-checking enabled by default via `.bazelrc`

## Structure

```
examples/nextjs-app/
  MODULE.bazel              # Workspace with rules_typescript + npm extension
  .bazelrc                  # Enables validation (--output_groups=+_validation)
  .bazelversion             # Pinned Bazel version
  pnpm-lock.yaml            # Locked npm deps (next, react, vitest)
  package.json              # npm manifest
  next.config.mjs           # Next.js config (minimal, no transpilePackages needed)
  tsconfig.json             # TypeScript config for Next.js
  BUILD.bazel               # node_modules + next_build + dev/serve + gazelle target
  app/
    layout.tsx              # Next.js root layout
    page.tsx                # Home page (imports from packages/shared)
    about/page.tsx          # About page, prerendered
    dynamic/page.tsx        # force-dynamic: rendered per request, echoes the Host
    api/hello/route.ts      # App Router route handler
  pages/
    legacy.tsx              # Pages Router page, prerendered
    ssr.tsx                 # getServerSideProps: rendered per request
    api/ping.ts             # Pages Router API route
  packages/
    shared/
      BUILD.bazel           # Package-level docstring (no rules — src/ is a sub-package)
      src/
        index.ts            # greet() + formatCurrency() utilities
        index.test.ts       # vitest tests for the shared utilities
        BUILD.bazel         # ts_compile:src + ts_test:src_test + filegroup:sources (Gazelle-generated)
```

## Quick start

```bash
bazel build //:app        # Next.js production build (produces .next/ output)
bazel run //:dev          # next dev, serving app/ and pages/ from source
bazel run //:serve        # next start over the build; try /dynamic and /ssr
bazel test //...          # vitest tests for shared packages
bazel run //:gazelle      # regenerate BUILD files from TypeScript sources
```

`/dynamic` and `/ssr` answer with the `Host` the request carried, and a second
request to `/dynamic` returns different HTML. That is the difference between a
server-rendered route and a prerendered file, and it is only visible with a
server running.

## How it works

The workspace uses a hybrid monorepo pattern with two distinct compilation strategies:

**Shared library (`packages/shared/src`)**: Compiled by `ts_compile`, which runs the oxc compiler to produce `.js` and `.d.ts` outputs. The `.d.ts` files form a compilation boundary: downstream packages see only the types, not the implementation. The `ts_test` target runs vitest tests against the shared logic. Gazelle auto-generates both the `ts_compile` and `ts_test` rules from the TypeScript sources.

**Next.js application (root `app/`)**: Built by `next_build`, which wraps `next build` as a single opaque Bazel action. The shared package sources are staged into the Next.js build directory via the `staging_srcs` attribute — they land at their workspace-relative paths (e.g. `packages/shared/src/index.ts`), so the relative import `../packages/shared/src/index` in `app/page.tsx` resolves correctly without any `transpilePackages` configuration.

Gazelle generates no TypeScript targets inside `app/`, `pages/`, `src/` or `public/` for a Next.js workspace: `next build` compiles them itself, and a BUILD file in one of them would make it a separate package that the root `glob()` in `next_build.srcs` could not reach into. Shared TypeScript lives outside those directories — `packages/shared` here — and reaches the build through `staging_srcs`.

**Running the app.** `next_dev_server` runs `next dev` with this directory as the project root, so an edit under `app/` or `pages/` reaches the browser through Next.js's own watcher rather than through a rebuild. `next_serve` runs `next start` over what `next_build` produced, staged into a writable directory with the config and `public/` beside it. Both find the app's dependencies through `NODE_PATH`, pointed at the Bazel-built npm tree, so neither plants a `node_modules` symlink in this directory.

## Gazelle round-trip

To verify Gazelle round-trip:

```bash
rm packages/shared/src/BUILD.bazel
bazel run //:gazelle
# BUILD.bazel is regenerated with ts_compile:src + ts_test:src_test
```

The `filegroup:sources` target in `packages/shared/src/BUILD.bazel` is not generated by Gazelle — it is manually maintained and Gazelle leaves it untouched on subsequent runs. This is intentional: the filegroup exports production sources for `next_build.staging_srcs` and must explicitly exclude test files.

## Using as a template

Copy this directory. Remove the `local_path_override` block in `MODULE.bazel` and set the `rules_typescript` version to the published BCR version. Keep `pnpm-lock.yaml` checked in — run `pnpm install` to update it when adding new npm dependencies.
