import type { Value07 } from "../pkg_07/types";
import type { Value08 } from "../pkg_08/types";
import type { Value12, Tag12 } from "./types";

export function compute12(id: string, raw: number, dep07: Value07, dep08: Value08): Value12 {
  return {
    id,
    value: raw * 12,
    base07: dep07,
    base08: dep08,
  };
}

export function label12(v: Value12): Tag12 {
  return `pkg12:${v.id}:${v.value}`;
}
