import type { Value06 } from "../pkg_06/types";
import type { Value07 } from "../pkg_07/types";
import type { Value11, Tag11 } from "./types";

export function compute11(id: string, raw: number, dep06: Value06, dep07: Value07): Value11 {
  return {
    id,
    value: raw * 11,
    base06: dep06,
    base07: dep07,
  };
}

export function label11(v: Value11): Tag11 {
  return `pkg11:${v.id}:${v.value}`;
}
