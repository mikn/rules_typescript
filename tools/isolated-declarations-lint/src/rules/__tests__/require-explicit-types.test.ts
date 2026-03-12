/**
 * Tests for the require-explicit-types ESLint rule.
 *
 * Uses @typescript-eslint/rule-tester with vitest as the test framework.
 * Run with: pnpm test
 */

import { RuleTester } from "@typescript-eslint/rule-tester";
import { afterAll, describe, it } from "vitest";
import { requireExplicitTypes } from "../require-explicit-types.js";

// RuleTester needs to be told which test framework it's running inside.
RuleTester.afterAll = afterAll;
RuleTester.describe = describe;
RuleTester.it = it;

const ruleTester = new RuleTester({
  languageOptions: {
    parserOptions: {
      // The rule does not require type information — it operates on the
      // syntactic AST only.
      ecmaVersion: 2022,
      sourceType: "module",
    },
  },
});

ruleTester.run("require-explicit-types", requireExplicitTypes, {
  // -------------------------------------------------------------------------
  // Valid — no errors expected
  // -------------------------------------------------------------------------
  valid: [
    // Function with explicit return type
    {
      code: `export function greet(name: string): string { return "hello " + name; }`,
    },
    // Arrow function assigned to typed variable
    {
      code: `export const add = (a: number, b: number): number => a + b;`,
    },
    // Variable with explicit type annotation
    {
      code: `export const VERSION: string = "1.0.0";`,
    },
    // Typed constant
    {
      code: `export const COUNT: number = 42;`,
    },
    // Class with typed method and property
    {
      code: `
        export class Counter {
          count: number = 0;
          increment(): void { this.count++; }
        }
      `,
    },
    // Non-exported items do not need annotations
    {
      code: `
        const helper = (x: number) => x * 2;
        function privateFunc() { return "no export"; }
        const localVar = 123;
      `,
    },
    // Already-annotated class method
    {
      code: `
        export class Greeter {
          name: string;
          constructor(name: string) { this.name = name; }
          greet(): string { return "hello " + this.name; }
        }
      `,
    },
    // Already-annotated arrow function variable
    {
      code: `export const getLabel = (): string => "label";`,
    },
    // Re-export of an already-typed identifier (no declaration to check)
    {
      code: `export { foo } from "./foo";`,
    },
    // Type-only export
    {
      code: `export type { Foo } from "./foo";`,
    },
    // Async function with explicit return type
    {
      code: `export async function fetchData(url: string): Promise<Response> { return fetch(url); }`,
    },
    // Generic function with explicit return type
    {
      code: `export function identity<T>(value: T): T { return value; }`,
    },
    // Default export ignored when ignoreDefaultExports is true
    {
      code: `export default function handler() { return "ok"; }`,
      options: [{ ignoreDefaultExports: true }],
    },
    // Declare keyword — no init value, no annotation needed at runtime
    {
      code: `export declare const x: string;`,
    },
  ],

  // -------------------------------------------------------------------------
  // Invalid — errors expected, some with auto-fix
  // -------------------------------------------------------------------------
  invalid: [
    // ---- Function declarations missing return type -------------------------

    // Simple function, auto-fixable (single string literal return)
    {
      code: `export function getVersion() { return "1.0.0"; }`,
      errors: [{ messageId: "missingReturnType" }],
      output: `export function getVersion(): string { return "1.0.0"; }`,
    },
    // Function returning a number literal
    {
      code: `export function getCount() { return 42; }`,
      errors: [{ messageId: "missingReturnType" }],
      output: `export function getCount(): number { return 42; }`,
    },
    // Function returning a boolean literal
    {
      code: `export function isEnabled() { return true; }`,
      errors: [{ messageId: "missingReturnType" }],
      output: `export function isEnabled(): boolean { return true; }`,
    },
    // Function returning void (empty return)
    {
      code: `export function doNothing() { return; }`,
      errors: [{ messageId: "missingReturnType" }],
      output: `export function doNothing(): void { return; }`,
    },
    // Function returning undefined identifier
    {
      code: `export function getUndefined() { return undefined; }`,
      errors: [{ messageId: "missingReturnType" }],
      output: `export function getUndefined(): undefined { return undefined; }`,
    },
    // Function with untyped parameter (reports two errors)
    {
      code: `export function greet(name) { return "hello"; }`,
      errors: [
        { messageId: "missingReturnType" },
        { messageId: "missingParameterType" },
      ],
      output: `export function greet(name): string { return "hello"; }`,
    },
    // Multi-statement body — cannot infer return type (no auto-fix)
    {
      code: `export function compute(x: number) { const y = x * 2; return y; }`,
      errors: [{ messageId: "missingReturnType" }],
      // No auto-fix: output equals input
      output: null,
    },

    // ---- Arrow function variables missing return type ----------------------

    {
      code: `export const getLabel = () => "label";`,
      errors: [{ messageId: "missingReturnType" }],
      output: `export const getLabel = (): string => "label";`,
    },
    {
      code: `export const getZero = () => 0;`,
      errors: [{ messageId: "missingReturnType" }],
      output: `export const getZero = (): number => 0;`,
    },
    {
      code: `export const isTrue = () => true;`,
      errors: [{ messageId: "missingReturnType" }],
      output: `export const isTrue = (): boolean => true;`,
    },

    // ---- Variable declarations missing type annotations -------------------

    {
      code: `export const VERSION = "1.0.0";`,
      errors: [{ messageId: "missingVariableType" }],
      output: `export const VERSION: string = "1.0.0";`,
    },
    {
      code: `export const TIMEOUT = 5000;`,
      errors: [{ messageId: "missingVariableType" }],
      output: `export const TIMEOUT: number = 5000;`,
    },
    {
      code: `export const IS_DEV = false;`,
      errors: [{ messageId: "missingVariableType" }],
      output: `export const IS_DEV: boolean = false;`,
    },
    {
      code: `export const EMPTY = null;`,
      errors: [{ messageId: "missingVariableType" }],
      output: `export const EMPTY: null = null;`,
    },
    {
      code: `export const NOTHING = undefined;`,
      errors: [{ messageId: "missingVariableType" }],
      output: `export const NOTHING: undefined = undefined;`,
    },
    // Array of uniform string literals
    {
      code: `export const TAGS = ["a", "b", "c"];`,
      errors: [{ messageId: "missingVariableType" }],
      output: `export const TAGS: string[] = ["a", "b", "c"];`,
    },
    // Array of uniform number literals
    {
      code: `export const SIZES = [1, 2, 3];`,
      errors: [{ messageId: "missingVariableType" }],
      output: `export const SIZES: number[] = [1, 2, 3];`,
    },
    // Empty array — inferred as never[]
    {
      code: `export const EMPTY_LIST = [];`,
      errors: [{ messageId: "missingVariableType" }],
      output: `export const EMPTY_LIST: never[] = [];`,
    },
    // Complex initialiser — no auto-fix
    {
      code: `export const result = someFunction();`,
      errors: [{ messageId: "missingVariableType" }],
      output: null,
    },

    // ---- Class members missing type annotations ---------------------------

    {
      code: `
        export class Greeter {
          name = "world";
          greet() { return "hello " + this.name; }
        }
      `,
      errors: [
        { messageId: "missingPropertyType" },
        { messageId: "missingReturnType" },
      ],
      output: null, // greet() has multi-stmt body; property fix not attempted
    },

    // ---- Default export missing return type --------------------------------

    {
      code: `export default function() { return 0; }`,
      errors: [{ messageId: "missingReturnType" }],
      output: `export default function(): number { return 0; }`,
    },
  ],
});
