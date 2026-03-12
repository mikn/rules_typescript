/**
 * Public API for the components package.
 *
 * This barrel export makes //src/components a package boundary: other packages
 * import from "../components" (directory) rather than individual files.
 */
export { Button } from "./Button";
export type { ButtonProps } from "./Button";
export { Counter } from "./Counter";
export type { CounterProps } from "./Counter";
