import { flavour } from "./esm.mjs";
import { legacyFlavour } from "./legacy.cjs";

export const esm: string = flavour("esm");
export const cjs: string = legacyFlavour("cjs");
export const ambient: "esm" = ESM_FLAVOUR;
