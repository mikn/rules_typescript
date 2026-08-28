/**
 * Tests for the `isolated-declarations/require-explicit-types` rule.
 *
 * Uses @typescript-eslint/rule-tester with vitest.
 */

import { RuleTester } from '@typescript-eslint/rule-tester';
import { afterAll, describe, it } from 'vitest';
import { requireExplicitTypes } from '../require-explicit-types.js';

// @typescript-eslint/rule-tester requires a test framework integration.
// With vitest we set `afterAll` directly.
RuleTester.afterAll = afterAll;
RuleTester.describe = describe;
RuleTester.it = it;

const ruleTester = new RuleTester({
  languageOptions: {
    parser: await import('@typescript-eslint/parser'),
    parserOptions: {
      ecmaVersion: 2022,
      sourceType: 'module',
    },
  },
});

ruleTester.run('require-explicit-types', requireExplicitTypes, {
  // ── Valid: cases that should NOT be flagged ─────────────────────────────
  valid: [
    {
      name: 'exported function with explicit return type',
      code: `export function add(a: number, b: number): number { return a + b; }`,
    },
    // The binding annotation already describes the signature. Flagging (and
    // auto-fixing) this would rewrite code that is correct as written.
    {
      name: 'exported arrow function with binding type annotation',
      code: `export const fn: () => string = () => 'hello';`,
    },
    {
      name: 'exported arrow function with return type on arrow',
      code: `export const fn = (): string => 'hello';`,
    },
    {
      name: 'exported function expression with return type',
      code: `export const fn = function(): number { return 42; };`,
    },
    {
      name: 'exported variable with explicit type',
      code: `export const name: string = 'rules_typescript';`,
    },
    {
      name: 'exported Map with explicit generic',
      code: `export const m: Map<string, number> = new Map();`,
    },
    {
      name: 'exported generic function with return type',
      code: `export function identity<T>(x: T): T { return x; }`,
    },
    {
      name: 'exported function with conditional return type',
      code: `export function wrap<T>(x: T): T extends string ? string : number { return x as never; }`,
    },
    {
      name: 'exported type alias',
      code: `export type Foo = { bar: string };`,
    },
    {
      name: 'exported interface',
      code: `export interface Config { port: number; }`,
    },
    {
      name: 'exported enum',
      code: `export enum Color { Red, Green, Blue }`,
    },
    {
      name: 'exported class',
      code: `export class Greeter { greet(): string { return 'hi'; } }`,
    },
    {
      name: 'named re-export',
      code: `export { foo } from './other.js';`,
    },
    {
      name: 'namespace re-export',
      code: `export * from './other.js';`,
    },
    {
      name: 'type-only named export',
      code: `import type { Foo } from './foo.js'; export type { Foo };`,
    },
    {
      name: 'default export function with return type',
      code: `export default function handler(): void { console.log('ok'); }`,
    },
    {
      name: 'default export identifier',
      code: `const val: number = 42; export default val;`,
    },
    {
      name: 'default export literal',
      code: `export default 42;`,
    },
    {
      name: 'default export class',
      code: `export default class MyClass {}`,
    },
    {
      name: 'function overloads with return types',
      code: `
        export function foo(x: string): string;
        export function foo(x: number): number;
        export function foo(x: string | number): string | number { return x; }
      `,
    },
    {
      name: 'declare function',
      code: `export declare function foo(): string;`,
    },
    {
      name: 'exported function with typed parameter and return type',
      code: `export function greet(name: string): string { return "hello " + name; }`,
    },
    {
      name: 'exported arrow with typed parameters and return type',
      code: `export const add = (a: number, b: number): number => a + b;`,
    },
    {
      name: 'exported string constant with explicit type',
      code: `export const VERSION: string = "1.0.0";`,
    },
    {
      name: 'exported number constant with explicit type',
      code: `export const COUNT: number = 42;`,
    },
    {
      name: 'exported class with typed property and typed method',
      code: `
        export class Counter {
          count: number = 0;
          increment(): void { this.count++; }
        }
      `,
    },
    {
      name: 'non-exported declarations are not checked',
      code: `
        const helper = (x: number) => x * 2;
        function privateFunc() { return "no export"; }
        const localVar = 123;
      `,
    },
    {
      name: 'exported class with constructor and typed members',
      code: `
        export class Greeter {
          name: string;
          constructor(name: string) { this.name = name; }
          greet(): string { return "hello " + this.name; }
        }
      `,
    },
    {
      name: 'exported arrow returning a literal with return type',
      code: `export const getLabel = (): string => "label";`,
    },
    {
      name: 'named re-export without file extension',
      code: `export { foo } from "./foo";`,
    },
    {
      name: 'type-only re-export',
      code: `export type { Foo } from "./foo";`,
    },
    {
      name: 'exported async function with explicit return type',
      code: `export async function fetchData(url: string): Promise<Response> { return fetch(url); }`,
    },
    {
      name: 'exported generic identity with return type',
      code: `export function identity<T>(value: T): T { return value; }`,
    },
    {
      name: 'default export ignored when ignoreDefaultExports is true',
      code: `export default function handler() { return "ok"; }`,
      options: [{ ignoreDefaultExports: true }],
    },
    {
      name: 'exported declare const',
      code: `export declare const x: string;`,
    },
    {
      name: 'setter with a typed parameter needs no return type (TS1095)',
      code: `export class Box { set value(v: number) { this.v = v; } }`,
    },
    {
      name: 'getter with a return type',
      code: `export class Box { get value(): number { return 1; } }`,
    },
  ],

  // ── Invalid: cases that SHOULD be flagged ───────────────────────────────
  invalid: [
    {
      name: 'async function is fixed to Promise<T>, not T (TS1064)',
      code: `export async function fetchOne() { return 1; }`,
      output: `export async function fetchOne(): Promise<number> { return 1; }`,
      errors: [{ messageId: 'missingReturnType' }],
    },
    {
      name: 'async arrow is fixed to Promise<T>',
      code: `export const fetchTwo = async () => "two";`,
      output: `export const fetchTwo = async (): Promise<string> => "two";`,
      errors: [{ messageId: 'missingReturnType' }],
    },
    {
      name: 'async method is fixed to Promise<T>',
      code: `export class Svc { async load() { return "ok"; } }`,
      output: `export class Svc { async load(): Promise<string> { return "ok"; } }`,
      errors: [{ messageId: 'missingReturnType' }],
    },
    {
      name: 'generator is reported but never autofixed',
      code: `export function* gen() { return 1; }`,
      output: null,
      errors: [{ messageId: 'missingReturnType' }],
    },
    {
      name: 'generator method is reported but never autofixed',
      code: `export class Svc { *iter() { return 0; } }`,
      output: null,
      errors: [{ messageId: 'missingReturnType' }],
    },
    {
      name: 'untyped setter parameter is reported, and never gets a return type',
      code: `export class Box { set value(v) { this.v = v; } }`,
      output: null,
      errors: [{ messageId: 'missingParameterType' }],
    },
    {
      name: 'getter without a return type is reported',
      code: `export class Box { get value() { return 1; } }`,
      output: `export class Box { get value(): number { return 1; } }`,
      errors: [{ messageId: 'missingReturnType' }],
    },
    // ---- Function declarations missing a return type -----------------------
    {
      name: 'function returning a string literal is auto-fixed',
      code: `export function getVersion() { return "1.0.0"; }`,
      errors: [{ messageId: 'missingReturnType' }],
      output: `export function getVersion(): string { return "1.0.0"; }`,
    },
    {
      name: 'function returning a number literal is auto-fixed',
      code: `export function getCount() { return 42; }`,
      errors: [{ messageId: 'missingReturnType' }],
      output: `export function getCount(): number { return 42; }`,
    },
    {
      name: 'function returning a boolean literal is auto-fixed',
      code: `export function isEnabled() { return true; }`,
      errors: [{ messageId: 'missingReturnType' }],
      output: `export function isEnabled(): boolean { return true; }`,
    },
    {
      name: 'bare return is auto-fixed to void',
      code: `export function doNothing() { return; }`,
      errors: [{ messageId: 'missingReturnType' }],
      output: `export function doNothing(): void { return; }`,
    },
    {
      name: 'function returning the undefined identifier is auto-fixed',
      code: `export function getUndefined() { return undefined; }`,
      errors: [{ messageId: 'missingReturnType' }],
      output: `export function getUndefined(): undefined { return undefined; }`,
    },
    {
      name: 'untyped parameter is reported alongside the return type',
      code: `export function greet(name) { return "hello"; }`,
      errors: [
        { messageId: 'missingReturnType' },
        { messageId: 'missingParameterType' },
      ],
      output: `export function greet(name): string { return "hello"; }`,
    },
    {
      name: 'multi-statement body cannot be inferred',
      code: `export function compute(x: number) { const y = x * 2; return y; }`,
      errors: [{ messageId: 'missingReturnType' }],
      output: null,
    },

    // ---- Arrow function bindings missing a return type ---------------------
    {
      name: 'arrow returning a string literal is auto-fixed',
      code: `export const getLabel = () => "label";`,
      errors: [{ messageId: 'missingReturnType' }],
      output: `export const getLabel = (): string => "label";`,
    },
    {
      name: 'arrow returning a number literal is auto-fixed',
      code: `export const getZero = () => 0;`,
      errors: [{ messageId: 'missingReturnType' }],
      output: `export const getZero = (): number => 0;`,
    },
    {
      name: 'arrow returning a boolean literal is auto-fixed',
      code: `export const isTrue = () => true;`,
      errors: [{ messageId: 'missingReturnType' }],
      output: `export const isTrue = (): boolean => true;`,
    },

    // ---- Variable declarations missing a type annotation -------------------
    {
      name: 'string initialiser is auto-fixed',
      code: `export const VERSION = "1.0.0";`,
      errors: [{ messageId: 'missingVariableType' }],
      output: `export const VERSION: string = "1.0.0";`,
    },
    {
      name: 'number initialiser is auto-fixed',
      code: `export const TIMEOUT = 5000;`,
      errors: [{ messageId: 'missingVariableType' }],
      output: `export const TIMEOUT: number = 5000;`,
    },
    {
      name: 'boolean initialiser is auto-fixed',
      code: `export const IS_DEV = false;`,
      errors: [{ messageId: 'missingVariableType' }],
      output: `export const IS_DEV: boolean = false;`,
    },
    {
      name: 'null initialiser is auto-fixed',
      code: `export const EMPTY = null;`,
      errors: [{ messageId: 'missingVariableType' }],
      output: `export const EMPTY: null = null;`,
    },
    {
      name: 'undefined initialiser is auto-fixed',
      code: `export const NOTHING = undefined;`,
      errors: [{ messageId: 'missingVariableType' }],
      output: `export const NOTHING: undefined = undefined;`,
    },
    {
      name: 'uniform string array is auto-fixed',
      code: `export const TAGS = ["a", "b", "c"];`,
      errors: [{ messageId: 'missingVariableType' }],
      output: `export const TAGS: string[] = ["a", "b", "c"];`,
    },
    {
      name: 'uniform number array is auto-fixed',
      code: `export const SIZES = [1, 2, 3];`,
      errors: [{ messageId: 'missingVariableType' }],
      output: `export const SIZES: number[] = [1, 2, 3];`,
    },
    {
      name: 'empty array is auto-fixed to never[]',
      code: `export const EMPTY_LIST = [];`,
      errors: [{ messageId: 'missingVariableType' }],
      output: `export const EMPTY_LIST: never[] = [];`,
    },
    {
      name: 'call-expression initialiser cannot be inferred',
      code: `export const result = someFunction();`,
      errors: [{ messageId: 'missingVariableType' }],
      output: null,
    },

    // ---- Class members missing type annotations ----------------------------
    {
      name: 'untyped class property and method are both reported',
      code: `
        export class Greeter {
          name = "world";
          greet() { return "hello " + this.name; }
        }
      `,
      errors: [
        { messageId: 'missingPropertyType' },
        { messageId: 'missingReturnType' },
      ],
      output: null,
    },

    // ---- Default exports ---------------------------------------------------
    {
      name: 'anonymous default function returning a number is auto-fixed',
      code: `export default function() { return 0; }`,
      errors: [{ messageId: 'missingReturnType' }],
      output: `export default function(): number { return 0; }`,
    },
    {
      name: 'exported function missing return type',
      code: `export function add(a: number, b: number) { return a + b; }`,
      errors: [{ messageId: 'missingReturnType', data: { name: 'add' } }],
      output: null,
    },
    {
      name: 'exported arrow function missing return type',
      code: `export const greet = (name: string) => 'Hello, ' + name;`,
      errors: [{ messageId: 'missingReturnType', data: { name: 'greet' } }],
      output: null,
    },
    {
      name: 'exported function expression missing return type',
      code: `export const fn = function(x: number) { return x * 2; };`,
      errors: [{ messageId: 'missingReturnType', data: { name: 'fn' } }],
      output: null,
    },
    {
      name: 'exported variable missing type annotation',
      code: `export const schema = { version: 1 };`,
      errors: [{ messageId: 'missingVariableType', data: { name: 'schema' } }],
      output: null,
    },
    {
      name: 'exported Map without explicit type',
      code: `export const m = new Map();`,
      errors: [{ messageId: 'missingVariableType', data: { name: 'm' } }],
      output: null,
    },
    {
      name: 'exported generic function missing return type',
      code: `export function identity<T>(x: T) { return x; }`,
      errors: [{ messageId: 'missingReturnType', data: { name: 'identity' } }],
      output: null,
    },
    {
      name: 'default export arrow function missing return type',
      code: `export default (x: number) => x * 2;`,
      errors: [{ messageId: 'missingReturnType', data: { name: 'default' } }],
      output: null,
    },
    {
      name: 'default export function declaration missing return type',
      code: `export default function handler() { return { ok: true }; }`,
      errors: [{ messageId: 'missingReturnType', data: { name: 'handler' } }],
      output: null,
    },
    {
      name: 'default export object literal',
      code: `export default { version: 1, name: 'app' };`,
      errors: [{ messageId: 'missingDefaultExportType' }],
      output: null,
    },
    {
      name: 'function overload implementation missing return type',
      code: `
        export function foo(x: string): string;
        export function foo(x: number): number;
        export function foo(x: string | number) { return x; }
      `,
      errors: [{ messageId: 'missingReturnType', data: { name: 'foo' } }],
      output: null,
    },
  ],
});
