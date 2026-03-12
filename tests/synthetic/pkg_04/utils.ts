import type { Value00 } from "../pkg_00/types";
import type { Value04, Tag04 } from "./types";

export function compute04(id: string, raw: number, dep00: Value00): Value04 {
  return {
    id,
    value: raw * 4,
    base00: dep00,
  };
}

export function label04(v: Value04): Tag04 {
  return `pkg04:${v.id}:${v.value}`;
}
