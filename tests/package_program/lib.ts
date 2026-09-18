import { greet } from "./greet.mjs";

export function double(n: number): number {
  return n * 2;
}

export const greeting: string = greet("lib");
