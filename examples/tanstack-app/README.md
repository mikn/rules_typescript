# tanstack-app example

A self-contained Bazel workspace demonstrating `rules_typescript` with a
TanStack Router application. It covers TSX compilation, type-safe routing,
Zod schema validation for route parameters, vitest tests, cross-package
dependencies, and Gazelle-generated BUILD files.

## Prerequisites

- [Bazel](https://bazel.build/) 9.0.0+
- [pnpm](https://pnpm.io/) (to update the lockfile; not needed just to build)

## Package layout

```
src/
  lib/           router.ts — router factory; params.ts — Zod route param schemas
  components/    Layout.tsx, UserCard.tsx — shared UI components
  routes/        __root.tsx, index.tsx, about.tsx, users.tsx — route definitions
  app/           application entry point, re-exports the router
```

## Workflow

This example follows the rules_typescript workflow exactly: write TypeScript,
run Gazelle, then build and test.

### 1. Write TypeScript

Source files live under `src/`. All exported values carry explicit return types
(required by oxc for isolated `.d.ts` emission).

**Factory function pattern for router types.** TanStack Router uses deeply nested
generics that are impractical to write by hand. The `lib/router.ts` module wraps
router creation in a factory function so the complex internal types stay private:

```typescript
// src/lib/router.ts
import type { AnyRouter } from "@tanstack/react-router";

export type AppRouter = AnyRouter;

function buildRouter(): AppRouter {
  const rootRoute = createRootRoute({ component: RootComponent });
  // ... internal route tree — no need for explicit generic annotations here
  return createRouter({ routeTree });
}

export const router: AppRouter = buildRouter();
```

This satisfies isolated declarations (the exported `router` constant has an
explicit type) while keeping the router creation code readable.

**Zod route parameter schemas** in `lib/params.ts` carry explicit generic
annotations so oxc can emit `.d.ts` without inference:

```typescript
export const UsersSearchSchema: z.ZodObject<{
  page: z.ZodDefault<z.ZodOptional<z.ZodNumber>>;
  search: z.ZodOptional<z.ZodString>;
}> = z.object({ ... });
```

### 2. Generate BUILD files with Gazelle

```bash
bazel run //:gazelle
```

Gazelle reads TypeScript imports and writes `ts_compile`, `ts_test`, and
`node_modules` targets. After generation, add runtime-only deps that Gazelle
cannot infer from static imports. For example, `src/lib/BUILD.bazel` needs
`@npm//:tanstack_react-router` and `@npm//:zod` in its `node_modules` target
so the vitest runner can resolve them at test time.

### 3. Build

```bash
bazel build //...
```

oxc compiles every `ts_compile` target and emits `.js` + `.d.ts` files. The
root `app_bundle` target bundles `src/app` into a single ESM file.

### 4. Type-check with tsgo

```bash
bazel build //... --output_groups=+_validation
```

tsgo runs as a separate validation action. Failures appear as build errors.

### 5. Run tests

```bash
bazel test //...
```

Two test suites run under vitest:

| Target | Tests |
|--------|-------|
| `//src/lib:lib_test` | router creation + AppRouter typing; Zod param schema parsing |
| `//src/components:components_test` | UserCard prop rendering |

## Key points

**`@tanstack/react-router` bundles its own types.** Unlike `react`, it ships
`.d.ts` files inside its own package directory, so no `@types/*` pairing is
needed. It is listed as a `ts_compile` dep and types resolve automatically.

**`@types/react` pairing still applies.** Even though TanStack Router provides
its own types, the JSX runtime types come from `@types/react`. List
`@npm//:react` as a dep and `rules_typescript` pairs it with `@npm//:types_react`
automatically.

**Scoped package label names.** In `deps`, the `@` sigil and `/` separator in
scoped npm package names are replaced with underscores: `@tanstack/react-router`
becomes `@npm//:tanstack_react-router`.

**JSX return types.** Use `import type { ReactElement, ReactNode } from "react"`
as return types for TSX components. `React.JSX.Element` is not a global in
`@types/react` 19.

**Isolated declarations.** oxc requires that every exported symbol has an
explicit type annotation. This constraint keeps compilation fast and hermetic.

## Updating npm dependencies

```bash
pnpm install          # generates pnpm-lock.yaml
bazel run //:gazelle  # re-sync BUILD files if new packages were added
bazel build //...
```

The lockfile is checked in so Bazel can reproduce the exact build without a
network connection (after the first fetch).
