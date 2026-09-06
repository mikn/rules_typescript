import { describe, expect, it } from "vitest";
import { accepts } from "../lib";

describe("lib", () => {
  it("runs the zod schema the lib declared", () => {
    expect(accepts({ size: "sm" })).toBe(true);
    expect(accepts({ size: "xl" })).toBe(false);
  });
});
