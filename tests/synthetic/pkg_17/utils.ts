import type { Value07 } from "../pkg_07/types";
import type { Value08 } from "../pkg_08/types";
import type { Value17, Tag17 } from "./types";

export function compute17(id: string, raw: number, dep07: Value07, dep08: Value08): Value17 {
  return {
    id,
    value: raw * 17,
    base07: dep07,
    base08: dep08,
  };
}

export function label17(v: Value17): Tag17 {
  return `pkg17:${v.id}:${v.value}`;
}
