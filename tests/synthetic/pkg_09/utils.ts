import type { Value01 } from "../pkg_01/types";
import type { Value09, Tag09 } from "./types";

export function compute09(id: string, raw: number, dep01: Value01): Value09 {
  return {
    id,
    value: raw * 9,
    base01: dep01,
  };
}

export function label09(v: Value09): Tag09 {
  return `pkg09:${v.id}:${v.value}`;
}
