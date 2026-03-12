// Button component that imports a CSS file.
// The CSS import is a side-effect import — no named bindings.
import "./button.css";

export interface ButtonProps {
  label: string;
  disabled?: boolean;
}

/**
 * Returns a display string describing the button state.
 * This function demonstrates that ts_compile handles .tsx + CSS deps correctly.
 */
export function describeButton(props: ButtonProps): string {
  const state = props.disabled ? "disabled" : "enabled";
  return `${props.label} (${state})`;
}

export const BUTTON_CLASS = "button" as const;
