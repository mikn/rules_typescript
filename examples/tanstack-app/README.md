# examples/tanstack-app

A client-side SPA with TanStack React Router, Zod route params, and Vite bundling.

## What this demonstrates

- TanStack React Router with type-safe routing
- Zod validation for route parameters
- Factory pattern to wrap complex router generics for isolated declarations
- `ts_bundle` with Vite for production SPA output (index.html + hashed assets)
- `ts_codegen` for TanStack Router route tree generation
- Scoped npm package labels (`@tanstack/react-router` -> `@npm//:tanstack_react-router`)
- vitest tests for router creation and param schemas
- tsgo type-checking (enabled by default in `.bazelrc`)
- Gazelle auto-generating BUILD files from TypeScript sources

## Structure

```
examples/tanstack-app/
  MODULE.bazel                # Workspace definition with npm extension
  .bazelrc                    # Enables validation (--output_groups=+_validation)
  pnpm-lock.yaml              # Locked npm deps
  index.html                  # SPA shell for Vite app-mode bundling
  BUILD.bazel                 # ts_binary, ts_bundle (Vite SPA), vite_bundler, gazelle
  generate-routes.sh          # Shell wrapper for TanStack route tree codegen
  tanstack-vite.config.mjs    # Experimental tanstackStart() Vite config
  src/
    lib/
      router.ts               # Router factory (wraps complex generics)
      params.ts               # Zod route parameter schemas
      router.test.ts           # Router creation test
      params.test.ts           # Param schema test
    components/
      Layout.tsx               # Root layout component
      UserCard.tsx             # User display component
      UserCard.test.tsx        # Component test
    routes/
      __root.tsx               # Root route (layout wrapper)
      index.tsx, about.tsx, users.tsx  # Page routes
    app/
      index.ts                 # Re-exports router
      main.tsx                 # React SPA entry point (createRoot + RouterProvider)
```

## Quick start

```bash
bazel build //...    # compile + type-check (validation is on by default via .bazelrc)
bazel test //...     # run vitest tests
bazel build //:spa   # produce deployable SPA bundle (index.html + JS)
bazel run //:gazelle # regenerate BUILD files from source
```

## How it works

The `//src/lib` package contains the router factory and Zod param schemas. TanStack Router uses deeply nested generics that are impractical to annotate by hand, so `router.ts` wraps creation in a factory function returning `AnyRouter` -- this satisfies isolated declarations while keeping the code readable. Zod schemas for route params carry explicit generic annotations (e.g., `z.ZodObject<{page: z.ZodDefault<...>}>`) so oxc can emit `.d.ts` without inference.

The `//src/routes` package defines the route tree using TSX components that depend on `@npm//:tanstack_react-router`. Unlike `react`, TanStack Router ships its own `.d.ts` files so no `@types` pairing is needed. However, JSX runtime types still come from `@types/react`, so `@npm//:react` is listed as a dep and rules_typescript pairs it with `@npm//:types_react` automatically.

The root `BUILD.bazel` defines both a `ts_binary` (plain ESM bundle) and a `ts_bundle` using `vite_bundler` for production SPA output. The `:spa` target runs Vite in app mode against `index.html` to produce a deployable directory with hashed assets. A `ts_codegen` target can generate `routeTree.gen.ts` from the route files using `@tanstack/router-generator`.

## Using as a template

Copy this directory. Remove the `local_path_override` block in `MODULE.bazel` and set the `rules_typescript` version to the published BCR version. Keep `pnpm-lock.yaml` checked in -- run `pnpm install` to update it when adding new npm dependencies. The `tanstack-vite.config.mjs` and `:spa_tanstack` target are experimental (SSR) and can be removed for a pure SPA setup.
