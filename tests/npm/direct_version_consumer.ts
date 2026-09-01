import stripAnsi from "strip-ansi";

export function clean(input: string): string {
  return stripAnsi(input);
}
