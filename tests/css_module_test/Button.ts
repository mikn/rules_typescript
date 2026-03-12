// Button component that imports CSS modules.
// Tests that class names are accessible as strings at test runtime.
import styles from "./Button.module.css";

export function getButtonClass(): string {
  return styles.button;
}

export function getContainerClass(): string {
  return styles.container;
}

export function getLabelClass(): string {
  return styles.label;
}
