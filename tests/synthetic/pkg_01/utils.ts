import type { Value00 } from "../pkg_00/types";
import type { Value01, Tag01 } from "./types";

export function compute01(id: string, raw: number, dep00: Value00): Value01 {
  return {
    id,
    value: raw * 1,
    base00: dep00,
  };
}

export function label01(v: Value01): Tag01 {
  return `pkg01:${v.id}:${v.value}`;
}
