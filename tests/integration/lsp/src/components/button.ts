// A simple component module importable as "@/components" or "@/components/button".
export interface ButtonProps {
  label: string;
  disabled?: boolean;
}

export function createButton(props: ButtonProps): ButtonProps {
  return { ...props };
}
