import { describe, expect, it } from "vitest";
import { answer, note } from "./lib.js";

describe("a dep's data srcs", () => {
  it("are beside the compiled module at run time", () => {
    expect(answer).toBe(42);
    expect(note()).toBe("the data src was beside its module");
  });
});
