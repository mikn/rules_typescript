import type { Value04 } from "../pkg_04/types";
import type { Value08, Tag08 } from "./types";

export function compute08(id: string, raw: number, dep04: Value04): Value08 {
  return {
    id,
    value: raw * 8,
    base04: dep04,
  };
}

export function label08(v: Value08): Tag08 {
  return `pkg08:${v.id}:${v.value}`;
}
