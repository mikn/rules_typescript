import type { Color } from "culori";
import { converter } from "culori/fn";

export function toRgb(color: Color): Color | undefined {
  return converter("rgb")(color);
}
