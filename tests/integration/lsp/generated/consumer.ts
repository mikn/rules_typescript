import { generatedValue } from "#generated/value";
import type { first } from "#types/first";
export const value: typeof first = generatedValue;
import { generatedValue as orderedValue } from "#ordered/value";
import { generatedValue as specificValue } from "#specific/value";
export const ordered: "authored" = orderedValue;
export const specific: "override" = specificValue;
