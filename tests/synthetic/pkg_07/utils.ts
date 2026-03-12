import type { Value03 } from "../pkg_03/types";
import type { Value07, Tag07 } from "./types";

export function compute07(id: string, raw: number, dep03: Value03): Value07 {
  return {
    id,
    value: raw * 7,
    base03: dep03,
  };
}

export function label07(v: Value07): Tag07 {
  return `pkg07:${v.id}:${v.value}`;
}
