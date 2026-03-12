import type { Value09 } from "../pkg_09/types";
import type { Value05 } from "../pkg_05/types";
import type { Value19, Tag19 } from "./types";

export function compute19(id: string, raw: number, dep09: Value09, dep05: Value05): Value19 {
  return {
    id,
    value: raw * 19,
    base09: dep09,
    base05: dep05,
  };
}

export function label19(v: Value19): Tag19 {
  return `pkg19:${v.id}:${v.value}`;
}
