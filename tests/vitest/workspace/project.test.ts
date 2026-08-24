import { describe, expect, it } from "vitest";
import { labelClass } from "./styles";

describe("vitest workspace project", () => {
  it("runs inside a named project", () => {
    expect(labelClass()).toBe("label");
  });
});
