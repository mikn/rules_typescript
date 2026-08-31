import { describe, expect, it } from "vitest";

describe("vite root", () => {
  it("resolves a config's relative path against the package", () => {
    expect(
      (globalThis as { __SETUP_FROM_PACKAGE_RELATIVE_PATH__?: boolean })
        .__SETUP_FROM_PACKAGE_RELATIVE_PATH__,
    ).toBe(true);
  });
});
