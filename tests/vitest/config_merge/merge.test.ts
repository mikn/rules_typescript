import { describe, expect, it } from "vitest";
import answer from "virtual:answer";
import { buttonClass } from "./styles";

describe("user config merges with the generated config", () => {
  it("resolves a virtual module from the user config's plugin", () => {
    expect(answer).toBe(42);
  });

  it("still answers CSS modules through the Bazel-owned plugin", () => {
    expect(buttonClass()).toMatch(/^_button_[0-9a-f]{8}$/);
  });
});
