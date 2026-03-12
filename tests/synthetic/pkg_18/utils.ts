import type { Value08 } from "../pkg_08/types";
import type { Value09 } from "../pkg_09/types";
import type { Value18, Tag18 } from "./types";

export function compute18(id: string, raw: number, dep08: Value08, dep09: Value09): Value18 {
  return {
    id,
    value: raw * 18,
    base08: dep08,
    base09: dep09,
  };
}

export function label18(v: Value18): Tag18 {
  return `pkg18:${v.id}:${v.value}`;
}
