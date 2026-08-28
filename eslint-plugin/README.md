# @rules_typescript/eslint-plugin-isolated-declarations

An ESLint plugin with one rule: `require-explicit-types`. It reports exports
whose types a `.d.ts` emitter cannot read straight off the syntax, which is
what TypeScript's [`isolatedDeclarations`](https://www.typescriptlang.org/tsconfig#isolatedDeclarations)
mode requires.

## Why isolated declarations wants this

Normally TypeScript writes `math.d.ts` by running type inference — it has to
work out what `add()` returns, which means loading this file, its imports,
their declarations, and so on. Under isolated declarations each file's `.d.ts`
is derived from that file's syntax alone, so declaration emit never waits for
an upstream type-check.

The catch is that a syntactic emitter faced with an un-annotated export cannot
fail loudly — it can only widen. An object of five `RegExp`s becomes `{}`; a
`RegExp` becomes `unknown`. The target still builds, and the damage surfaces
later in a consumer, against the wrong file:

```
parseDomain.ts(95,51): error TS2339: Property 'idPreview' does not exist on type '{}'.
parseDomain.ts(96,41): error TS18046: 'UUID_PATTERN' is of type 'unknown'.
```

This rule reports the missing annotations at their own source location, in your
editor, before that happens.

## Install

The plugin is not on the public npm registry yet. Build it from the
[`eslint-plugin/`](https://github.com/mikn/rules_typescript/tree/main/eslint-plugin)
directory of the `rules_typescript` repository and install the tarball:

```bash
# in a rules_typescript checkout:
cd eslint-plugin
npm install && npm run build && npm pack
# -> rules_typescript-eslint-plugin-isolated-declarations-0.1.0.tgz

# in your project:
npm install --save-dev \
  /path/to/rules_typescript/eslint-plugin/rules_typescript-eslint-plugin-isolated-declarations-0.1.0.tgz \
  @typescript-eslint/parser \
  eslint
```

Once it is published, the same two peers plus
`@rules_typescript/eslint-plugin-isolated-declarations` by name is all it takes.

Requires ESLint 9+ (flat config) and `@typescript-eslint/parser` 8+, both peer
dependencies. The rule is purely syntactic: it reads the AST only, so no
`parserOptions.project` and no type information are needed.

## Usage

Take the bundled config:

```js
// eslint.config.js
import isolatedDeclarations from '@rules_typescript/eslint-plugin-isolated-declarations';

export default [isolatedDeclarations.configs.recommended];
```

`configs.recommended` registers the plugin under the `isolated-declarations`
namespace and turns `require-explicit-types` on at `error`. It is the only
bundled config, and it is deliberately all-or-nothing: isolated declarations is
a per-package property, and a package with one un-annotated export does not
have it.

Or wire it up yourself, if you want a different severity, namespace, or file
scope:

```js
import isolatedDeclarations from '@rules_typescript/eslint-plugin-isolated-declarations';

export default [
  {
    files: ['src/**/*.ts'],
    plugins: { 'isolated-declarations': isolatedDeclarations },
    rules: { 'isolated-declarations/require-explicit-types': 'error' },
  },
];
```

The default export is the plugin object. `plugin` is also a named export, and
`requireExplicitTypes` is exported directly for consumers who assemble their own
rule sets.

Both an ESM and a CommonJS entry point ship. Because the entry has named
exports alongside the default one, `require()` hands back the module namespace,
so a CommonJS config reaches the plugin through `.default`:

```js
// eslint.config.cjs
const isolatedDeclarations =
  require('@rules_typescript/eslint-plugin-isolated-declarations').default;

module.exports = [isolatedDeclarations.configs.recommended];
```

## What `require-explicit-types` flags

| Export pattern | Reported when |
|----------------|---------------|
| `export function foo() {}` | No `: ReturnType` on the signature |
| `export const fn = () => ...` | No return type on the arrow, and none on the binding |
| `export const x = expr` | No `: Type` on the binding |
| `export function foo(a) {}` | A parameter has no `: Type` |
| `export class Foo { bar = 1 }` | A property, method return type, or method parameter is unannotated |
| `export default function () {}` | No `: ReturnType` on the signature |
| `export default { a: 1 }` | An expression default export with no type context |

Where the type is readable straight off the AST, the report carries an
auto-fix, so `eslint --fix` annotates it: literals (`string`, `number`,
`boolean`, `null`, `bigint`), template literals, `undefined`/`NaN`/`Infinity`,
negated and signed literals, array literals whose elements are all inferable
(`never[]` when empty, `T[]` when uniform, a union of up to four members
otherwise), and function bodies that are a single `return` of one of those (or
a bare `return`, giving `void`).

Everything else is reported with a suggestion telling you to annotate by hand.
Object literals are never inferred — a structural annotation synthesised from
one is verbose and silently drops optionality and method signatures. Parameters
and class properties are likewise never auto-fixed.

### What it does not flag

`export type`, `export interface`, `export enum`, `export declare`, re-exports
(`export { x } from './y'`), a default export of a bare identifier or a literal,
constructors, setter return types (annotating one is a compile error, TS1095),
and computed class members.

That list is why a clean run of this rule is a good signal and not a guarantee:
the emitter you point at the package is the authority on whether its
declarations can be emitted syntactically.

## Options

One option, on a schema with `additionalProperties: false`:

| Option | Type | Default | Effect |
|--------|------|---------|--------|
| `ignoreDefaultExports` | `boolean` | `false` | Skip every `export default` form |

```js
rules: {
  'isolated-declarations/require-explicit-types': [
    'error',
    { ignoreDefaultExports: true },
  ],
}
```

## Licence

MIT. The full text ships in this package as `LICENSE`, and is the same licence
as the `rules_typescript` repository this package lives in.
