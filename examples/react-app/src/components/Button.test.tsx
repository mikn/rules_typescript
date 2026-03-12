import { describe, expect, it, vi } from "vitest";

import { Button } from "./Button";
import type { ButtonProps } from "./Button";
import type { MouseEvent } from "react";

describe("ButtonProps interface", () => {
  it("accepts minimal props", () => {
    // Type-level test: verify the interface shape is correct.
    const props: ButtonProps = {
      label: "Click me",
      onClick: (_e: MouseEvent<HTMLButtonElement>): void => {},
    };
    expect(props.label).toBe("Click me");
    expect(props.disabled).toBeUndefined();
    expect(props.variant).toBeUndefined();
  });

  it("accepts all props", () => {
    const spy = vi.fn();
    const props: ButtonProps = {
      label: "Submit",
      onClick: spy,
      disabled: true,
      variant: "primary",
    };
    expect(props.disabled).toBe(true);
    expect(props.variant).toBe("primary");
  });

  it("accepts variant options", () => {
    const variants: Array<ButtonProps["variant"]> = [
      "primary",
      "secondary",
      "danger",
    ];
    expect(variants).toHaveLength(3);
  });
});

describe("Button function", () => {
  it("is a function", () => {
    expect(typeof Button).toBe("function");
  });
});
