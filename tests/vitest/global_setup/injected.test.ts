import { describe, expect, inject, it } from "vitest";

declare module "vitest" {
  interface ProvidedContext {
    rulesTsGlobalSetup: string;
  }
}

describe("global_setup", () => {
  it("reads a value the global setup file provided", () => {
    expect(inject("rulesTsGlobalSetup")).toBe("ran");
  });
});
