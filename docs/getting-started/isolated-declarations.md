# Isolated Declarations

This is the concept that makes everything fast. Read this before writing code.

## What It Means

Normally TypeScript generates `.d.ts` declaration files by running full type inference across your project — it needs to know the return type of `add()` in `math.ts` before it can write `math.d.ts`. That means changing any `.ts` file can potentially invalidate `.d.ts` files across the project, forcing Bazel to recompile downstream packages.

With isolated declarations, each file's `.d.ts` is generated from that file alone, with no cross-file inference — because you wrote the return types explicitly. This is the architectural keystone: if you change `math.ts` without changing its exported types, its `.d.ts` is identical. Bazel sees no change at the dependency boundary and skips all downstream packages.

## The Requirement

```typescript
// This fails with isolated_declarations = True (return type is inferred)
export function add(a: number, b: number) {
  return a + b;
}

// This works (explicit return type)
export function add(a: number, b: number): number {
  return a + b;
}
```

The rule applies to every exported function, arrow function, and variable. The tsgo type-checker reports missing annotations as:

```
error TS9007: Declaration emit for this file requires type resolution. ...
```

## What Happens Without It

Setting `isolated_declarations = False` (via the Gazelle directive or the rule attribute) tells oxc to fall back to emitting `.d.ts` with inferred types. The build still works, you still get hermetic caching, but the `.d.ts` boundary is less precise: a change that doesn't affect public types may still cause downstream recompilation because Bazel can't prove the `.d.ts` is unchanged.

## What the ESLint Rule Covers

| Export pattern | Flagged when |
|----------------|-------------|
| `export function foo() {}` | No `: ReturnType` annotation |
| `export const fn = () => ...` | No return type on arrow or binding annotation |
| `export const x = someExpression` | No `: Type` annotation on binding |
| `export default function() {}` | No `: ReturnType` annotation |

The rule does NOT flag `export type`, `export interface`, `export class`, `export enum`, re-exports (`export { x } from '...'`), or ambient declarations.

## Migration

If you have an existing codebase, start with `# gazelle:ts_isolated_declarations false` (see [Quick Start — Path B](quickstart.md#path-b-existing-project)) and migrate one package at a time.

**Step 1.** Install the ESLint plugin that reports missing annotations.

The plugin is not yet published to npm. Build it from the `eslint-plugin/` directory in the `rules_typescript` repository and install the resulting tarball:

```bash
# From the rules_typescript checkout:
cd path/to/rules_typescript/eslint-plugin
npm install
npm pack
# This produces rules_typescript-eslint-plugin-isolated-declarations-0.1.0.tgz

# In your project:
npm install --save-dev \
  path/to/rules_typescript/eslint-plugin/rules_typescript-eslint-plugin-isolated-declarations-0.1.0.tgz \
  @typescript-eslint/parser \
  eslint
```

Configure it in `eslint.config.js`:

```js
import isolatedDeclarations from '@rules_typescript/eslint-plugin-isolated-declarations';

export default [
  {
    plugins: { 'isolated-declarations': isolatedDeclarations },
    rules: { 'isolated-declarations/require-explicit-types': 'error' },
  },
];
```

**Step 2.** Pick one package. Run the linter on it:

```bash
npx eslint src/my-package/
```

**Step 3.** Fix the reported violations — add explicit return types and type annotations to all exported symbols.

**Step 4.** Add `# gazelle:ts_isolated_declarations true` to that package's `BUILD.bazel` (or remove the `false` directive if you set it per-directory). Re-run Gazelle to regenerate:

```bash
bazel run //:gazelle
bazel build //src/my-package --output_groups=+_validation
```

**Step 5.** Repeat for the next package. Each migrated package immediately benefits from the faster incremental boundary.
