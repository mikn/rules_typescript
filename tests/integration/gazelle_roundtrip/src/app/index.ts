import { formatHex } from "culori";

import { add, format, multiply } from "../lib";

export const mode: string = import.meta.env.MODE;

// culori ships no declarations; @types/culori types this through the pairing.
export const brand: string = formatHex("red") ?? "";

export function main(): string {
  const sum: number = add(1, 2);
  const product: number = multiply(3, 4);
  return `Result: ${format(sum)} and ${product}`;
}
