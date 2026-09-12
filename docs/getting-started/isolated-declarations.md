# Isolated Declarations

An opt-in throughput mode, one value for the whole build:
`--//ts:declarations=oxc`. The default is `--//ts:declarations=tsgo`.

Earlier versions required the opt-in mode. The tsgo action builds a complete
type program per target to type-check, and emits the declarations from it;
`oxc` is the opt-in path for the extra throughput.

## What It Means

TypeScript normally writes `math.d.ts` by running type inference: it has to know
what `add()` returns. The emit therefore needs the whole program, meaning this
file, its imports, their `.d.ts`, and so on.

With isolated declarations, each file's `.d.ts` is derived from that file's
syntax alone, because every export carries an explicit type. No program, no
dependency types, no inference.

## What It Buys

Both modes cache the same way: change `math.ts` without changing its exported
types and the emitted `.d.ts` is byte-identical, so Bazel skips every downstream
target.

The mode buys pipelining. Oxc emits a `.d.ts` without a type program, so a
dependent waits for a per-file transform rather than for `TsgoDeclare`, tsgo's
declaration emit over the whole upstream program; the type check is a
validation action nothing blocks on in either mode. On a deep dependency chain
that shortens the critical path substantially; shallower graphs narrow the gap,
deeper ones widen it. `tools/bench_declarations.sh 20 50 3` measures it on
1,000 annotated files across 20 packages in one linear chain
([Cost of Each Mode](../rules/ts-compile.md#cost-of-each-mode)); run it on your
own graph.

## The Requirement

```typescript
// Rejected under --//ts:declarations=oxc — the return type is inferred
export function add(a: number, b: number) {
  return a + b;
}

// Accepted
export function add(a: number, b: number): number {
  return a + b;
}
```

It applies to every exported function, arrow function, and variable. Oxc reports
violations itself and fails the build:

```
× Isolated declarations error(s): TS9007: Function must have an explicit
│ return type annotation with --isolatedDeclarations.
```

The build fails because a syntactic emitter can only widen an un-annotated
export: an object of five `RegExp`s becomes `{}`, a `RegExp` becomes `unknown`,
and the target still builds. Oxc did not check and tsgo saw valid TypeScript,
so nothing reports it and the error surfaces later in a consumer, against the
wrong file:

```
parseDomain.ts(95,51): error TS2339: Property 'idPreview' does not exist on type '{}'.
parseDomain.ts(96,41): error TS18046: 'UUID_PATTERN' is of type 'unknown'.
```

The mode has no partial version. The flag is one value for the build, so either
every package's exports are annotated and the build uses `oxc`, or it stays on
the default. A rule that wants one subtree under the other value writes a
Starlark transition on the flag; `tests/flags.bzl` is the ruleset's own.

## What the ESLint Rule Covers

| Export pattern | Flagged when |
|----------------|-------------|
| `export function foo() {}` | No `: ReturnType` annotation |
| `export const fn = () => ...` | No return type on arrow or binding annotation |
| `export const x = someExpression` | No `: Type` annotation on binding |
| `export function foo(a) {}` | A parameter has no `: Type` annotation |
| `export class Foo { bar = 1 }` | A property or method of the class is unannotated |
| `export default function() {}` | No `: ReturnType` annotation |
| `export default { a: 1 }` | Expression default export with no type context |

Where the type is readable straight off the AST (a literal, a uniform array
literal, a single-`return` body) the report carries an auto-fix, so
`eslint --fix` annotates it. Everything else is reported with a suggestion to
annotate by hand; the rule never guesses an object literal's shape.

The rule does NOT flag `export type`, `export interface`, `export enum`,
re-exports (`export { x } from '...'`), or ambient declarations, so a clean lint
run does not guarantee a clean `oxc` build. Oxc is the authority.

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

## Migration

Every build starts on `--//ts:declarations=tsgo` and builds. Move over when
throughput matters and every package's exports are annotated.

**Step 1.** Install the ESLint plugin that reports missing annotations.

The plugin is not yet published to npm. Build it from the `eslint-plugin/`
directory in the `rules_typescript` repository and install the tarball:

```bash
# From the rules_typescript checkout:
cd path/to/rules_typescript/eslint-plugin
npm install
npm run build   # tsup -> dist/
npm pack
# produces rules_typescript-eslint-plugin-isolated-declarations-<version>.tgz,
# where <version> is the `version` in eslint-plugin/package.json

# In your project:
npm install --save-dev \
  path/to/rules_typescript/eslint-plugin/rules_typescript-eslint-plugin-isolated-declarations-0.2.0.tgz \
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

The bundled config, `isolatedDeclarations.configs.recommended`, does the same.

**Step 2.** Run the linter over the repository:

```bash
npx eslint .
```

**Step 3.** Add the missing explicit types. Annotate accurately: a `: any` or an
over-wide annotation degrades the declaration as much as the widening this mode
prevents.

**Step 4.** Switch the build over in `.bazelrc` and build:

```
build --@rules_typescript//ts:declarations=oxc
```

```bash
bazel build //...
```

If anything was missed, Oxc fails the build and names the file and line.
