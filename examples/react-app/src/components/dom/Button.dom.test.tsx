/**
 * DOM tests for the Button component using @testing-library/react.
 *
 * This test runs under the happy-dom environment (configured in vitest.config.mjs)
 * and uses @testing-library/react to render the Button component and assert on
 * its DOM output.
 *
 * The ts_test target (in dom/BUILD.bazel) has its own node_modules target that
 * includes happy-dom, @testing-library/react, and all @vitest/* sub-packages.
 */

import { render, screen, fireEvent, cleanup } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

// Explicitly clean up the DOM after each test.
// @testing-library/react auto-cleanup requires the correct vitest environment
// integration; calling it explicitly ensures it always runs.
afterEach(() => {
  cleanup();
});

import { Button } from "../Button";

describe("Button DOM rendering", () => {
  it("renders the label text", () => {
    render(<Button label="Click me" onClick={() => {}} />);
    expect(screen.getByText("Click me")).toBeDefined();
  });

  it("renders as a button element", () => {
    render(<Button label="Submit" onClick={() => {}} />);
    const el = screen.getByRole("button");
    expect(el).toBeDefined();
    expect(el.tagName.toLowerCase()).toBe("button");
  });

  it("applies the variant class", () => {
    render(<Button label="Primary" onClick={() => {}} variant="primary" />);
    const el = screen.getByRole("button");
    expect(el.className).toContain("btn-primary");
  });

  it("applies secondary variant class", () => {
    render(<Button label="Secondary" onClick={() => {}} variant="secondary" />);
    const el = screen.getByRole("button");
    expect(el.className).toContain("btn-secondary");
  });

  it("is disabled when disabled prop is true", () => {
    render(<Button label="Disabled" onClick={() => {}} disabled={true} />);
    const el = screen.getByRole("button") as HTMLButtonElement;
    expect(el.disabled).toBe(true);
  });

  it("is not disabled by default", () => {
    render(<Button label="Active" onClick={() => {}} />);
    const el = screen.getByRole("button") as HTMLButtonElement;
    expect(el.disabled).toBe(false);
  });

  it("calls onClick when clicked", () => {
    const handler = vi.fn();
    render(<Button label="Click" onClick={handler} />);
    fireEvent.click(screen.getByRole("button"));
    expect(handler).toHaveBeenCalledTimes(1);
  });

  it("does not call onClick when disabled and clicked", () => {
    const handler = vi.fn();
    render(<Button label="Disabled" onClick={handler} disabled={true} />);
    fireEvent.click(screen.getByRole("button"));
    // The button is disabled so the click event should not trigger the handler.
    expect(handler).not.toHaveBeenCalled();
  });
});
