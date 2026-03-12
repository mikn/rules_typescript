# examples/react-app

A React component library with TSX compilation, DOM testing, and Vite bundling.

## What this demonstrates

- TSX compilation with oxc (React components)
- `@types/react` automatic pairing (types resolved from `@npm//:types_react`)
- Custom React hooks with cross-package deps
- Zod validation as a separate package
- vitest unit tests and `@testing-library/react` DOM tests (happy-dom)
- `ts_binary` bundling to a single ESM file
- `ts_dev_server` skeleton for development
- tsgo type-checking (enabled by default in `.bazelrc`)
- Gazelle auto-generating BUILD files from TypeScript sources

## Structure

```
examples/react-app/
  MODULE.bazel            # Workspace definition with npm extension
  .bazelrc                # Enables validation (--output_groups=+_validation)
  pnpm-lock.yaml          # Locked npm deps
  BUILD.bazel             # ts_binary bundle + gazelle target
  src/
    hooks/
      useCounter.ts       # Custom React hook
      useCounter.test.ts  # Hook unit test
    components/
      Button.tsx          # Stateless component with ButtonProps interface
      Counter.tsx         # Stateful component using useCounter hook
      Button.test.tsx     # Component unit test
      dom/
        Button.dom.test.tsx   # @testing-library/react DOM test
        vitest.config.mjs     # happy-dom environment config
    validation/
      userSchema.ts       # Zod schema with explicit type annotations
      userSchema.test.ts  # Schema validation test
    app/
      App.tsx             # Root React component
      index.ts            # Barrel re-export
```

## Quick start

```bash
bazel build //...    # compile + type-check (validation is on by default via .bazelrc)
bazel test //...     # run vitest tests (unit + DOM)
bazel run //:gazelle # regenerate BUILD files from source
```

## How it works

Four packages demonstrate the React component library pattern. `//src/hooks` defines a `useCounter` hook with `@npm//:react` as a dep. `//src/components` depends on `//src/hooks` and `@npm//:react` for TSX compilation -- when `@npm//:react` appears in deps, rules_typescript automatically pairs it with `@npm//:types_react` for type resolution. `//src/validation` uses `zod` independently. `//src/app` composes them into the root `App.tsx`.

The DOM test in `src/components/dom/` uses `@testing-library/react` under a happy-dom vitest environment. It lives in a sub-package with an explicit `node_modules` target because the DOM test runner needs packages like `react-dom` and `@testing-library/react` that the unit tests do not. Unit tests elsewhere use `ts_test` which auto-generates `node_modules` from deps.

JSX return types require `import type { ReactElement } from "react"` because `React.JSX.Element` is not a global in `@types/react` 19. All exported symbols need explicit type annotations for oxc's isolated declarations mode.

## Using as a template

Copy this directory. Remove the `local_path_override` block in `MODULE.bazel` and set the `rules_typescript` version to the published BCR version. Keep `pnpm-lock.yaml` checked in -- run `pnpm install` to update it when adding new npm dependencies.
