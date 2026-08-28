# `isolated-declarations/require-explicit-types`

Reports exports whose types a `.d.ts` emitter cannot read straight off the
syntax, which is what TypeScript's
[`isolatedDeclarations`](https://www.typescriptlang.org/tsconfig#isolatedDeclarations)
mode requires. Purely syntactic: no `parserOptions.project`, no type
information.

Install and configuration: [the package README](../../README.md).

## Rule details

Reported:

```ts
export function add(a: number, b: number) { return a + b; }  // no return type
export const greeting = 'hello';                             // no binding type
export const fn = () => compute();                           // no return type
export function log(message) {}                              // untyped parameter
export class Point { x = 0; }                                // untyped property
export default { a: 1 };                                     // untyped expression
```

Not reported:

```ts
export function add(a: number, b: number): number { return a + b; }
export const greeting: string = 'hello';
export const fn: () => string = () => 'hello';   // the binding types the signature
export type Id = string;
export interface Config { debug: boolean }
export declare const version: string;
export { helper } from './helper.js';
export default identity;                         // a named binding, typed at its own site
```

Also skipped: constructors, computed class members, and setter return types —
annotating a setter is a compile error (TS1095), so reporting one would fix
working code into broken code.

## Auto-fixes and suggestions

A report carries an auto-fix when the type is readable off the AST: literals
(`string`, `number`, `boolean`, `null`, `bigint`), template literals,
`undefined`/`NaN`/`Infinity`, negated and signed literals, array literals whose
elements are all inferable (`never[]` when empty, `T[]` when uniform, a union of
up to four members otherwise), and function bodies that are a single `return` of
one of those — or a bare `return`, giving `void`.

Everything else gets a suggestion telling you to annotate by hand. Object
literals are never inferred: a structural annotation synthesised from one is
verbose and silently drops optionality and method signatures. Parameters and
class properties are never auto-fixed.

## Options

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

## When not to use it

When the package stays on an inference-based declaration emitter. Under that
mode the annotations buy nothing the type-check was not already computing.
