# @rules_typescript/eslint-plugin-isolated-declarations

An ESLint plugin with one rule: `require-explicit-types`. It reports exports
whose types a `.d.ts` emitter cannot read straight off the syntax, which is
what TypeScript's [`isolatedDeclarations`](https://www.typescriptlang.org/tsconfig#isolatedDeclarations)
mode requires.

## Isolated Declarations

Under `isolatedDeclarations` each file's `.d.ts` is derived from that file's
syntax alone, so declaration emit never waits for an upstream type-check.
Inference-based emit writes `math.d.ts` by working out what `add()` returns,
which means loading this file, its imports, and their declarations.

A syntactic emitter faced with an un-annotated export widens it. An object of
five `RegExp`s becomes `{}`; a `RegExp` becomes `unknown`. The target still
builds, and the error surfaces later in a consumer, against the wrong file:

```
parseDomain.ts(95,51): error TS2339: Property 'idPreview' does not exist on type '{}'.
parseDomain.ts(96,41): error TS18046: 'UUID_PATTERN' is of type 'unknown'.
```

This rule reports the missing annotations at their own source location, in your
editor.

## Install

The plugin is not on the public npm registry yet. Build it from the
[`eslint-plugin/`](https://github.com/mikn/rules_typescript/tree/main/eslint-plugin)
directory of the `rules_typescript` repository and install the tarball:

```bash
# in a rules_typescript checkout:
cd eslint-plugin
npm install && npm run build && npm pack
# -> rules_typescript-eslint-plugin-isolated-declarations-0.2.0.tgz

# in your project:
npm install --save-dev \
  /path/to/rules_typescript/eslint-plugin/rules_typescript-eslint-plugin-isolated-declarations-0.2.0.tgz \
  @typescript-eslint/parser \
  eslint
```

Once it is published, install
`@rules_typescript/eslint-plugin-isolated-declarations` by name alongside the
same two peers.

Requires ESLint 9+ (flat config) and `@typescript-eslint/parser` 8+, both peer
dependencies. The rule is purely syntactic: it reads the AST only, so no
`parserOptions.project` and no type information are needed.

## Usage

The bundled config:

```js
// eslint.config.js
import isolatedDeclarations from '@rules_typescript/eslint-plugin-isolated-declarations';

export default [isolatedDeclarations.configs.recommended];
```

`configs.recommended` registers the plugin under the `isolated-declarations`
namespace and turns `require-explicit-types` on at `error`. It is the only
bundled config.

For a different severity, namespace, or file scope, wire the plugin up
directly:

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

Both an ESM and a CommonJS entry point ship. The entry has named exports
alongside the default one, so `require()` hands back the module namespace, and a
CommonJS config reaches the plugin through `.default`:

```js
// eslint.config.cjs
const isolatedDeclarations =
  require('@rules_typescript/eslint-plugin-isolated-declarations').default;

module.exports = [isolatedDeclarations.configs.recommended];
```

## Reported Exports

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
a bare `return`, giving `void`). An `async` function's inferred type is wrapped
in `Promise<>`; a generator gets no fix.

A binding or return type that cannot be read off the AST is reported with a
suggestion to annotate by hand. Object literals are never inferred: a structural
annotation synthesised from one is verbose and drops optionality and method
signatures. Parameters, class properties and an expression default export carry
neither a fix nor a suggestion.

### Not Reported

`export type`, `export interface`, `export enum`, `export declare`, re-exports
(`export { x } from './y'`), a default export of a bare identifier or a literal,
constructors, setter return types (annotating one is a compile error, TS1095),
and computed class members.

A clean run of this rule does not guarantee the package emits declarations
syntactically; the emitter itself decides that.

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
