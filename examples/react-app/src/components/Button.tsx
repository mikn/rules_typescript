/**
 * Button component demonstrating TSX compilation with React.
 *
 * All props use explicit type annotations so that oxc can emit .d.ts files
 * without cross-file type inference (isolatedDeclarations requirement).
 *
 * JSX is transformed by oxc using the react-jsx runtime transform.
 * @types/react is paired automatically with the `react` npm target.
 */

import type { MouseEvent, ReactElement } from "react";

export interface ButtonProps {
  label: string;
  onClick: (event: MouseEvent<HTMLButtonElement>) => void;
  disabled?: boolean;
  variant?: "primary" | "secondary" | "danger";
}

export function Button(props: ButtonProps): ReactElement {
  const { label, onClick, disabled = false, variant = "primary" } = props;

  return (
    <button
      className={`btn btn-${variant}`}
      disabled={disabled}
      onClick={onClick}
    >
      {label}
    </button>
  );
}
