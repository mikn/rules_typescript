import type { Value09 } from "../pkg_09/types";
import type { Value05 } from "../pkg_05/types";
import type { Value14, Tag14 } from "./types";

export function compute14(id: string, raw: number, dep09: Value09, dep05: Value05): Value14 {
  return {
    id,
    value: raw * 14,
    base09: dep09,
    base05: dep05,
  };
}

export function label14(v: Value14): Tag14 {
  return `pkg14:${v.id}:${v.value}`;
}
