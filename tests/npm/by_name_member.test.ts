import { describe, expect, it } from "vitest";
import { answer, twice } from "by-name-member";

describe("a member's `.ts` subpath into a sibling member", () => {
  it("re-exports the sibling's value through the view", () => {
    expect(answer).toBe(42);
  });

  it("runs the member's own code over it", () => {
    expect(twice()).toBe(84);
  });
});
