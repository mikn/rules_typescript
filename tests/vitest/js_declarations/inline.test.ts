import { expect, it } from "vitest";
import { compile } from "./compile.mjs";

it("resolves ./compile.mjs to the compile.d.mts in its own srcs", () => {
  const out: string = compile(2);
  expect(out).toBe("2");
});
