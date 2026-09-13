import { double } from "./lib";

export { greeting } from "./lib";

export function quadruple(n: number): number {
  return double(double(n));
}
