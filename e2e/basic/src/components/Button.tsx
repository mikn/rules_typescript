/**
 * A minimal button component that exercises TSX compilation.
 *
 * JSX is transformed by oxc-bazel using the `react-jsx` runtime transform
 * (the default jsx_mode).  All types carry explicit annotations so that
 * isolated declarations can emit .d.ts without cross-file inference.
 */

export interface ButtonProps {
  label: string;
  onClick: () => void;
  disabled?: boolean;
}

export function Button(props: ButtonProps): JSX.Element {
  return (
    <button disabled={props.disabled} onClick={props.onClick}>
      {props.label}
    </button>
  );
}
