// Test that CSS module imports work in Node.js test environment.
// The auto-generated vitest config answers a .module.css import with the export
// map css_module wrote, so these are the class names a bundler emits.
import { describe, it, expect } from "vitest";
import { getButtonClass, getContainerClass, getLabelClass } from "./Button";

describe("CSS module import in Node test", () => {
  it("returns a non-empty string for button class", () => {
    const cls = getButtonClass();
    expect(typeof cls).toBe("string");
    expect(cls.length).toBeGreaterThan(0);
  });

  it("returns a non-empty string for container class", () => {
    const cls = getContainerClass();
    expect(typeof cls).toBe("string");
    expect(cls.length).toBeGreaterThan(0);
  });

  it("returns a non-empty string for label class", () => {
    const cls = getLabelClass();
    expect(typeof cls).toBe("string");
    expect(cls.length).toBeGreaterThan(0);
  });

  it("each class name is distinct", () => {
    const button = getButtonClass();
    const container = getContainerClass();
    const label = getLabelClass();
    expect(button).not.toBe(container);
    expect(button).not.toBe(label);
    expect(container).not.toBe(label);
  });
});
