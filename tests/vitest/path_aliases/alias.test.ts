import {describe, expect, it} from "vitest";

import type {Point} from "@shared/value";

describe("path alias", () => {
  it("type-checks a value against the aliased declaration", () => {
    const p: Point = {x: 1, y: 2};
    expect(p.x + p.y).toBe(3);
  });
});
