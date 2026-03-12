export interface ButtonProps {
  label: string;
  onClick: () => void;
}

export function Button(props: ButtonProps): JSX.Element {
  return <button onClick={props.onClick}>{props.label}</button>;
}
