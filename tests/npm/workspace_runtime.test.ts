import { describe, expect, it } from "vitest";
import { greet } from "shared";
import { frame } from "shared/wire";

describe("a workspace link", () => {
  it("resolves as a bare specifier at run time", () => {
    expect(greet("World")).toBe("Hello, World!");
  });

  it("resolves an exports subpath at run time", () => {
    expect(frame("x")).toBe("[x]");
  });

  it("brings the member's own npm dependencies with it", () => {
    expect(() => greet("")).toThrow();
  });
});
