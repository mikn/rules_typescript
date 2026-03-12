# react-app example

A self-contained Bazel workspace demonstrating `rules_typescript` with a React
application. It covers TSX compilation, `@types/react` pairing, custom hooks,
Zod validation, vitest tests, cross-package dependencies, and Gazelle-generated
BUILD files.

## Prerequisites

- [Bazel](https://bazel.build/) 9.0.0+
- [pnpm](https://pnpm.io/) (to update the lockfile; not needed just to build)

## Package layout

```
src/
  app/           App.tsx — root React component, re-exports the app
  components/    Button.tsx, Counter.tsx — reusable UI components
  hooks/         useCounter.ts — custom React hook
  validation/    userSchema.ts — Zod user schema with helper functions
```

## Workflow

This example follows the rules_typescript workflow exactly: write TypeScript,
run Gazelle, then build and test.

### 1. Write TypeScript

Source files live under `src/`. All exported values carry explicit return types
(required by oxc for isolated `.d.ts` emission). For example:

```typescript
// src/components/Button.tsx
import type { MouseEvent, ReactElement } from "react";

export interface ButtonProps {
  label: string;
  onClick?: (event: MouseEvent<HTMLButtonElement>) => void;
  disabled?: boolean;
}

export function Button({ label, onClick, disabled = false }: ButtonProps): ReactElement {
  return <button onClick={onClick} disabled={disabled}>{label}</button>;
}
```

### 2. Generate BUILD files with Gazelle

```bash
bazel run //:gazelle
```

Gazelle reads TypeScript imports and writes `ts_compile`, `ts_test`, and
`node_modules` targets in every `src/*/` directory. After generation you may
need to add runtime-only deps that Gazelle cannot infer from static imports —
see `src/hooks/BUILD.bazel` for an example where `@npm//:react` is added to the
`node_modules` target so the vitest runner can resolve it at test time.

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

tsgo runs as a separate validation action. Failures appear as build errors. This
step is optional during development but recommended in CI.

### 5. Run tests

```bash
bazel test //...
```

Three test suites run under vitest:

| Target | Tests |
|--------|-------|
| `//src/components:components_test` | ButtonProps interface + Button function |
| `//src/hooks:hooks_test` | useCounter initial state and increment |
| `//src/validation:validation_test` | Zod schema parsing and error handling |

## Key points

**`@types/react` pairing.** `@npm//:react` ships no `.d.ts` files — types come
from `@npm//:types_react`. When `@npm//:react` is listed as a `ts_compile` dep,
`rules_typescript` automatically pairs it with `@npm//:types_react` and redirects
tsgo's module resolution to the `@types/react` directory.

**JSX return types.** `React.JSX.Element` is not a global in `@types/react` 19.
Use `import type { ReactElement } from "react"` as the return type for TSX
components instead.

**Isolated declarations.** oxc requires that every exported symbol has an
explicit type annotation so it can emit `.d.ts` files without cross-file
inference. This constraint keeps compilation fast and hermetic.

## Updating npm dependencies

```bash
pnpm install          # generates pnpm-lock.yaml
bazel run //:gazelle  # re-sync BUILD files if new packages were added
bazel build //...
```

The lockfile is checked in so Bazel can reproduce the exact build without a
network connection (after the first fetch).
