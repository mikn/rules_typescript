import { add, format, multiply } from "../lib";

export function main(): string {
  const sum: number = add(1, 2);
  const product: number = multiply(3, 4);
  return `Result: ${format(sum)} and ${product}`;
}
