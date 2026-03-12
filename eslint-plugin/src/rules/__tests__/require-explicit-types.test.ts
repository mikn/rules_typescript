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
    // Function with explicit return type.
    {
      name: 'exported function with explicit return type',
      code: `export function add(a: number, b: number): number { return a + b; }`,
    },
    // Arrow function with return type annotation on the binding.
    {
      name: 'exported arrow function with binding type annotation',
      code: `export const fn: () => string = () => 'hello';`,
    },
    // Arrow function with return type on the arrow itself.
    {
      name: 'exported arrow function with return type on arrow',
      code: `export const fn = (): string => 'hello';`,
    },
    // Function expression with return type.
    {
      name: 'exported function expression with return type',
      code: `export const fn = function(): number { return 42; };`,
    },
    // Explicit variable type annotation.
    {
      name: 'exported variable with explicit type',
      code: `export const name: string = 'rules_typescript';`,
    },
    // Map with explicit generic type.
    {
      name: 'exported Map with explicit generic',
      code: `export const m: Map<string, number> = new Map();`,
    },
    // Generic function with return type.
    {
      name: 'exported generic function with return type',
      code: `export function identity<T>(x: T): T { return x; }`,
    },
    // Conditional return type.
    {
      name: 'exported function with conditional return type',
      code: `export function wrap<T>(x: T): T extends string ? string : number { return x as never; }`,
    },
    // Type alias export — never flagged.
    {
      name: 'exported type alias',
      code: `export type Foo = { bar: string };`,
    },
    // Interface export — never flagged.
    {
      name: 'exported interface',
      code: `export interface Config { port: number; }`,
    },
    // Enum export — never flagged.
    {
      name: 'exported enum',
      code: `export enum Color { Red, Green, Blue }`,
    },
    // Class export — never flagged.
    {
      name: 'exported class',
      code: `export class Greeter { greet(): string { return 'hi'; } }`,
    },
    // Re-export — never flagged (no new binding in this module).
    {
      name: 'named re-export',
      code: `export { foo } from './other.js';`,
    },
    // Namespace re-export.
    {
      name: 'namespace re-export',
      code: `export * from './other.js';`,
    },
    // Type-only export.
    {
      name: 'type-only named export',
      code: `import type { Foo } from './foo.js'; export type { Foo };`,
    },
    // Default export: function with return type.
    {
      name: 'default export function with return type',
      code: `export default function handler(): void { console.log('ok'); }`,
    },
    // Default export: identifier (declared elsewhere).
    {
      name: 'default export identifier',
      code: `const val: number = 42; export default val;`,
    },
    // Default export: literal.
    {
      name: 'default export literal',
      code: `export default 42;`,
    },
    // Default export: class.
    {
      name: 'default export class',
      code: `export default class MyClass {}`,
    },
    // Overload signature followed by implementation — both have return types.
    {
      name: 'function overloads with return types',
      code: `
        export function foo(x: string): string;
        export function foo(x: number): number;
        export function foo(x: string | number): string | number { return x; }
      `,
    },
    // Declare ambient function — never flagged.
    {
      name: 'declare function',
      code: `export declare function foo(): string;`,
    },
  ],

  // ── Invalid: cases that SHOULD be flagged ───────────────────────────────
  invalid: [
    // Function without return type.
    {
      name: 'exported function missing return type',
      code: `export function add(a: number, b: number) { return a + b; }`,
      errors: [
        {
          messageId: 'missingFunctionReturnType',
          data: { name: 'add' },
        },
      ],
    },
    // Arrow function without any type annotation.
    {
      name: 'exported arrow function missing return type',
      code: `export const greet = (name: string) => 'Hello, ' + name;`,
      errors: [
        {
          messageId: 'missingFunctionReturnType',
          data: { name: 'greet' },
        },
      ],
    },
    // Function expression without return type.
    {
      name: 'exported function expression missing return type',
      code: `export const fn = function(x: number) { return x * 2; };`,
      errors: [
        {
          messageId: 'missingFunctionReturnType',
          data: { name: 'fn' },
        },
      ],
    },
    // Variable without type annotation.
    {
      name: 'exported variable missing type annotation',
      code: `export const schema = { version: 1 };`,
      errors: [
        {
          messageId: 'missingVariableType',
          data: { name: 'schema' },
        },
      ],
    },
    // Map without explicit type (common zod / builder pattern).
    {
      name: 'exported Map without explicit type',
      code: `export const m = new Map();`,
      errors: [
        {
          messageId: 'missingVariableType',
          data: { name: 'm' },
        },
      ],
    },
    // Generic function missing return type.
    {
      name: 'exported generic function missing return type',
      code: `export function identity<T>(x: T) { return x; }`,
      errors: [
        {
          messageId: 'missingFunctionReturnType',
          data: { name: 'identity' },
        },
      ],
    },
    // Default export: anonymous arrow without return type.
    {
      name: 'default export arrow function missing return type',
      code: `export default (x: number) => x * 2;`,
      errors: [
        {
          messageId: 'missingDefaultExportType',
        },
      ],
    },
    // Default export: function declaration without return type.
    {
      name: 'default export function declaration missing return type',
      code: `export default function handler() { return { ok: true }; }`,
      errors: [
        {
          messageId: 'missingFunctionReturnType',
          data: { name: 'handler' },
        },
      ],
    },
    // Default export: object literal (no type context).
    {
      name: 'default export object literal',
      code: `export default { version: 1, name: 'app' };`,
      errors: [
        {
          messageId: 'missingDefaultExportType',
        },
      ],
    },
    // Implementation signature without return type when overloads are present.
    {
      name: 'function overload implementation missing return type',
      code: `
        export function foo(x: string): string;
        export function foo(x: number): number;
        export function foo(x: string | number) { return x; }
      `,
      errors: [
        {
          messageId: 'missingFunctionReturnType',
          data: { name: 'foo' },
        },
      ],
    },
  ],
});
