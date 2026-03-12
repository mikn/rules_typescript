import type { Value06 } from "../pkg_06/types";
import type { Value07 } from "../pkg_07/types";
import type { Value16, Tag16 } from "./types";

export function compute16(id: string, raw: number, dep06: Value06, dep07: Value07): Value16 {
  return {
    id,
    value: raw * 16,
    base06: dep06,
    base07: dep07,
  };
}

export function label16(v: Value16): Tag16 {
  return `pkg16:${v.id}:${v.value}`;
}
