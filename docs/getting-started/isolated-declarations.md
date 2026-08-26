# Isolated Declarations

An opt-in throughput mode. You do not need it to use this ruleset, and you do
not need to read this before writing code.

Earlier versions required it. That was a mistake: the tsgo action already built
a complete type program per target in order to type-check, so demanding
explicit annotations on every export bought nothing that was not already being
computed. tsgo now emits the declarations it derives, and
`declarations = "oxc"` is the opt-in path for teams that want the extra
throughput.

## What it means

Normally TypeScript writes `math.d.ts` by running type inference — it has to
know what `add()` returns. That means the emit needs the whole program: this
file, its imports, their `.d.ts`, and so on.

With isolated declarations, each file's `.d.ts` is derived from that file's
syntax alone, because every export carries an explicit type. No program, no
dependency types, no inference.

## What that actually buys

Not boundary precision. Under either mode, if you change `math.ts` without
changing its exported types, the emitted `.d.ts` is byte-identical and Bazel
skips every downstream target. That caching property is the same.

What it buys is **pipelining**. Because Oxc can emit a `.d.ts` without a type
program, declaration emit never waits for an upstream type-check, and
type-checking becomes a validation action that nothing blocks on. On a deep
dependency chain that shortens the critical path substantially:

| Mode | Rebuild wall | Critical path |
|------|--------------|---------------|
| `declarations = "tsgo"` (default) | 6.3s | 4.89s |
| `declarations = "oxc"` | 3.8s | 2.15s |
| `declarations = "oxc"`, `enable_check = False` | 2.7s | 1.06s |

One machine, one day, `tools/bench_declarations.sh 20 50 3`: 1,000 annotated
files across 20 packages in one linear chain, medians of three interleaved runs.
Shallower graphs narrow the gap; deeper ones widen it. The script is committed —
run it on your own graph rather than trusting these figures.

## The requirement

```typescript
// Rejected under declarations = "oxc" — the return type is inferred
export function add(a: number, b: number) {
  return a + b;
}

// Accepted
export function add(a: number, b: number): number {
  return a + b;
}
```

It applies to every exported function, arrow function, and variable. Oxc
reports violations itself and fails the build:

```
× Isolated declarations error(s): TS9013: Expression type can't be inferred
│ with --isolatedDeclarations.
```

This is a hard error, not a warning, and that is deliberate. A syntactic
emitter faced with an un-annotated export can only widen it — an object of five
`RegExp`s becomes `{}`, a `RegExp` becomes `unknown` — and the target still
builds. Nothing reports it, because Oxc did not check and tsgo saw source that
is perfectly valid TypeScript. The damage surfaces later in a consumer,
against the wrong file:

```
parseDomain.ts(95,51): error TS2339: Property 'idPreview' does not exist on type '{}'.
parseDomain.ts(96,41): error TS18046: 'UUID_PATTERN' is of type 'unknown'.
```

So there is no partial version of this mode. Either a package's exports are
annotated and it can use `"oxc"`, or it stays on the default.

## What the ESLint rule covers

| Export pattern | Flagged when |
|----------------|-------------|
| `export function foo() {}` | No `: ReturnType` annotation |
| `export const fn = () => ...` | No return type on arrow or binding annotation |
| `export const x = someExpression` | No `: Type` annotation on binding |
| `export function foo(a) {}` | A parameter has no `: Type` annotation |
| `export class Foo { bar = 1 }` | A property or method of the class is unannotated |
| `export default function() {}` | No `: ReturnType` annotation |
| `export default { a: 1 }` | Expression default export with no type context |

Where the type is readable straight off the AST — a literal, a uniform array
literal, a single-`return` body — the report carries an auto-fix, so
`eslint --fix` annotates it. Everything else is reported with a suggestion
telling you to annotate by hand; the rule never guesses an object literal's
shape.

The rule does NOT flag `export type`, `export interface`, `export enum`,
re-exports (`export { x } from '...'`), or ambient declarations — so a clean
lint run does not guarantee a clean `"oxc"` build. Oxc is the authority.

### Options

`ignoreDefaultExports` (default `false`) skips every `export default` form:

```js
rules: {
  'isolated-declarations/require-explicit-types': [
    'error',
    { ignoreDefaultExports: true },
  ],
}
```

## Adopting it, one package at a time

Nothing is required up front: every package starts on `declarations = "tsgo"`
and builds. Move a package over when its throughput matters.

**Step 1.** Install the ESLint plugin that reports missing annotations.

The plugin is not yet published to npm. Build it from the `eslint-plugin/`
directory in the `rules_typescript` repository and install the tarball:

```bash
# From the rules_typescript checkout:
cd path/to/rules_typescript/eslint-plugin
npm install
npm run build   # tsup -> dist/
npm pack
# produces rules_typescript-eslint-plugin-isolated-declarations-0.1.0.tgz

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

Or take the bundled config, `isolatedDeclarations.configs.recommended`.

**Step 2.** Pick one package and run the linter on it:

```bash
npx eslint src/my-package/
```

**Step 3.** Add the missing explicit types. Annotate them accurately — a lazy
`: any` or an over-wide annotation degrades the declaration exactly as much as
the widening this mode exists to prevent.

**Step 4.** Switch that package over with
`# gazelle:ts_declarations oxc` in its `BUILD.bazel`, then regenerate and build:

```bash
bazel run //:gazelle
bazel build //src/my-package
```

If anything was missed, Oxc fails the build and names the file and line.

**Step 5.** Repeat for the next package. Mixed modes across a workspace are
fine — declarations are declarations, whoever emitted them.
