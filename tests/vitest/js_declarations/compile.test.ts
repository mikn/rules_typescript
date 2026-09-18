import { expect, it } from "vitest";
import { compile } from "./compile.mjs";

// The dep compiles the untyped .mjs, which infers `any`; `false` fits this
// annotation only when the import resolved to compile.d.mts instead.
type IsAny<T> = 0 extends 1 & T ? true : false;
const declared: IsAny<ReturnType<typeof compile>> = false;

it("runs the .mjs that compile.d.mts declares", () => {
  expect(declared).toBe(false);
  expect(compile(1)).toBe("1");
});
