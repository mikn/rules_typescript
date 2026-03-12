import type { Value02 } from "../pkg_02/types";
import type { Value06, Tag06 } from "./types";

export function compute06(id: string, raw: number, dep02: Value02): Value06 {
  return {
    id,
    value: raw * 6,
    base02: dep02,
  };
}

export function label06(v: Value06): Tag06 {
  return `pkg06:${v.id}:${v.value}`;
}
