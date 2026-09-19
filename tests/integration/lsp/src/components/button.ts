import type { User } from "@/models/user";

// A component module importable as "@/components/button"; its owner type comes
// through the tsconfig's alias.
export interface ButtonProps {
  label: string;
  owner: User;
  disabled?: boolean;
}

export function createButton(props: ButtonProps): ButtonProps {
  return { ...props };
}
