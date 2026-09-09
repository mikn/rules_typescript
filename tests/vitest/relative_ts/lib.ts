import { double, utilUrl } from "./deep/util.ts";

export const libUrl = import.meta.url;
export { utilUrl };

export function quadruple(n: number): number {
  return double(double(n));
}
