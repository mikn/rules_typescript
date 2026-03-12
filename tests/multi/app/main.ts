import { add, multiply } from "../lib/math";

export function calculate(x: number, y: number): number {
  return add(x, y) + multiply(x, y);
}
