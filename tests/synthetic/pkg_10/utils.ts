import type { Value05 } from "../pkg_05/types";
import type { Value06 } from "../pkg_06/types";
import type { Value10, Tag10 } from "./types";

export function compute10(id: string, raw: number, dep05: Value05, dep06: Value06): Value10 {
  return {
    id,
    value: raw * 10,
    base05: dep05,
    base06: dep06,
  };
}

export function label10(v: Value10): Tag10 {
  return `pkg10:${v.id}:${v.value}`;
}
