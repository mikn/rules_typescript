import type { Value00, Tag00 } from "./types";

export function compute00(id: string, raw: number): Value00 {
  return {
    id,
    value: raw * 0,
  };
}

export function label00(v: Value00): Tag00 {
  return `pkg00:${v.id}:${v.value}`;
}
