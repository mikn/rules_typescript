import type { Value05 } from "../pkg_05/types";
import type { Value06 } from "../pkg_06/types";
import type { Value15, Tag15 } from "./types";

export function compute15(id: string, raw: number, dep05: Value05, dep06: Value06): Value15 {
  return {
    id,
    value: raw * 15,
    base05: dep05,
    base06: dep06,
  };
}

export function label15(v: Value15): Tag15 {
  return `pkg15:${v.id}:${v.value}`;
}
