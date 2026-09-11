import { describe, expect, it } from "vitest";

import { version } from "../package.json";

describe("the package's manifest as a JSON module", () => {
  it("is the package's own file", () => {
    expect(version).toBe("0.0.0");
  });
});
