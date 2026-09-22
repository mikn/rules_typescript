import { expect, it } from "vitest";
import { compile } from "./compile.mjs";

it("runs the .mjs that compile.d.mts declares", () => {
  expect(compile(1)).toBe("1");
});
