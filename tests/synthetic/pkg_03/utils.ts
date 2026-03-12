import type { Value00 } from "../pkg_00/types";
import type { Value03, Tag03 } from "./types";

export function compute03(id: string, raw: number, dep00: Value00): Value03 {
  return {
    id,
    value: raw * 3,
    base00: dep00,
  };
}

export function label03(v: Value03): Tag03 {
  return `pkg03:${v.id}:${v.value}`;
}
