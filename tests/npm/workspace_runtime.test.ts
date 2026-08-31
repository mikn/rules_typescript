import { describe, expect, it } from "vitest";
import { greet } from "shared";

describe("a workspace link", () => {
  it("resolves as a bare specifier at run time", () => {
    expect(greet("World")).toBe("Hello, World!");
  });

  it("brings the member's own npm dependencies with it", () => {
    expect(() => greet("")).toThrow();
  });
});
