// culori ships no declarations; @types/culori holds them, and `culori/fn` is a
// subpath only the @types package declares.
import type { Color } from "culori";
import { converter } from "culori/fn";

export const toRgb = (color: Color): Color | undefined => converter("rgb")(color);
