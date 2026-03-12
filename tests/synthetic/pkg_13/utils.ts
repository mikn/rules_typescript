import type { Value08 } from "../pkg_08/types";
import type { Value09 } from "../pkg_09/types";
import type { Value13, Tag13 } from "./types";

export function compute13(id: string, raw: number, dep08: Value08, dep09: Value09): Value13 {
  return {
    id,
    value: raw * 13,
    base08: dep08,
    base09: dep09,
  };
}

export function label13(v: Value13): Tag13 {
  return `pkg13:${v.id}:${v.value}`;
}
