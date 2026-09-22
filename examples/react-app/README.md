# examples/react-app

A React component library with TSX compilation, DOM testing, and Vite bundling.

## What This Demonstrates

- TSX compilation with oxc (React components)
- `@types/react` automatic pairing (types resolved from `@npm//:types_react`)
- Custom React hooks with cross-package deps
- Zod validation as a separate package
- vitest unit tests and `@testing-library/react` DOM tests (happy-dom)
- `ts_binary` bundling to a single ESM file
- `ts_dev_server` skeleton for development
- tsgo type-checking (enabled by default in `.bazelrc`)
- Gazelle writing the BUILD files, one package per `tsconfig.json` program

## Structure

```
examples/react-app/
  MODULE.bazel            # Workspace definition with npm extension
  .bazelrc                # Enables validation (--output_groups=+_validation)
  .bazelignore            # node_modules, the installed tree Gazelle lists
  pnpm-lock.yaml          # Locked npm deps
  tsconfig.json           # The options every package extends; no program
  BUILD.bazel             # ts_binary bundle, gazelle + pnpm, root importer
  src/
    hooks/
      tsconfig.json       # Extends the root's: the package's program
      useCounter.ts       # Custom React hook
      useCounter.test.ts  # Hook unit test
    components/
      tsconfig.json
      Button.tsx          # Stateless component with ButtonProps interface
      Counter.tsx         # Stateful component using useCounter hook
      Button.test.tsx     # Component unit test
      dom/
        tsconfig.json         # A package of its own: the config below is its
        Button.dom.test.tsx   # @testing-library/react DOM test
        vitest.config.mjs     # happy-dom environment config
    validation/
      tsconfig.json
      userSchema.ts       # Zod schema with explicit type annotations
      userSchema.test.ts  # Schema validation test
    app/
      tsconfig.json
      App.tsx             # Root React component
      index.ts            # Barrel re-export
```

## Quick Start

```bash
bazel build //...    # compile + type-check (validation is on by default via .bazelrc)
bazel test //...     # run vitest tests (unit + DOM)
bazel run //:pnpm -- install --frozen-lockfile  # the tree Gazelle lists through
bazel run //:gazelle # write the BUILD files from the tsconfig.json programs
```

## How It Works

Four packages demonstrate the React component library pattern. `//src/hooks` defines a `useCounter` hook with `@npm//:react` as a dep. `//src/components` depends on `//src/hooks` and `@npm//:react` for TSX compilation -- when `@npm//:react` appears in deps, rules_typescript automatically pairs it with `@npm//:types_react` for type resolution. `//src/validation` uses `zod` independently. `//src/app` composes them into the root `App.tsx`.

The DOM test in `src/components/dom/` uses `@testing-library/react` under the
happy-dom environment its `vitest.config.mjs` sets. It is a package of its own,
with its own `tsconfig.json`, because a vitest config is the package's. Every
`ts_test` runs in the importer chain its `node_modules` names -- the root
`node_modules` target, the root importer's links into the store -- and its deps
carry the root `package.json`'s dependencies beside the imports tsgo lists.

Gazelle writes one package per `tsconfig.json` program: each
`src/*/tsconfig.json` extends the root `tsconfig.json`, which sets the compiler
options (`jsx: react-jsx` among them) and lists no files, so it is no program of
its own. A run lists each program through the installed `node_modules`, so
`bazel run //:pnpm -- install --frozen-lockfile` precedes it; `bazel run
//:gazelle -- -mode=diff` prints nothing on this tree.

JSX return types require `import type { ReactElement } from "react"` because `React.JSX.Element` is not a global in `@types/react` 19. All exported symbols need explicit type annotations for oxc's isolated declarations mode.

## Using as a Template

Copy this directory. Remove the `local_path_override` block in `MODULE.bazel` and set the `rules_typescript` version to the published BCR version. Keep `pnpm-lock.yaml` checked in -- run `pnpm install` to update it when adding new npm dependencies.
