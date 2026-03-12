import type { Value01 } from "../pkg_01/types";
import type { Value05, Tag05 } from "./types";

export function compute05(id: string, raw: number, dep01: Value01): Value05 {
  return {
    id,
    value: raw * 5,
    base01: dep01,
  };
}

export function label05(v: Value05): Tag05 {
  return `pkg05:${v.id}:${v.value}`;
}
