// Button component that imports a CSS Module.
// The default import gives typed access to scoped class names.
import styles from "./Button.module.css";

export interface ButtonProps {
  label: string;
  disabled?: boolean;
}

/**
 * Returns the CSS class string for the button element.
 * Demonstrates that TypeScript accepts the CSS Module default import
 * and provides typed access to class names.
 */
export function getButtonClass(props: ButtonProps): string {
  if (props.disabled) {
    return `${styles.button} ${styles.disabled}`;
  }
  return styles.button;
}

export function getContainerClass(): string {
  return styles.container;
}

export const LABEL_CLASS: string = styles.label;
