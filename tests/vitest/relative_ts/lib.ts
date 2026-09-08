import { double } from "./deep/util.ts";

export function quadruple(n: number): number {
  return double(double(n));
}
