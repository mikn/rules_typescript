import { describe, expect, it } from "vitest";
import { prefersWide, setupOrder } from "./media";

describe("setup_files", () => {
  it("makes a global available that only the setup file installs", () => {
    expect(prefersWide()).toBe(true);
  });

  it("runs setup files in the order they are listed", () => {
    expect(setupOrder()).toEqual(["polyfills", "order"]);
  });
});
