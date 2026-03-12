import type { Value00 } from "../pkg_00/types";
import type { Value02, Tag02 } from "./types";

export function compute02(id: string, raw: number, dep00: Value00): Value02 {
  return {
    id,
    value: raw * 2,
    base00: dep00,
  };
}

export function label02(v: Value02): Tag02 {
  return `pkg02:${v.id}:${v.value}`;
}
