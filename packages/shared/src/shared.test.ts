import { describe, expect, it } from "vitest";
import { greet } from "shared";
import { frame } from "shared/wire";

describe("the member imported by its own name", () => {
  it("reaches the entry the exports map designates", () => {
    expect(greet("World")).toBe("Hello, World!");
  });

  it("reaches the file an exports subpath designates", () => {
    expect(frame("x")).toBe("[x]");
  });
});
