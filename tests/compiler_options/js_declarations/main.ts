import { flavour } from "./esm.mjs";
import { legacyFlavour } from "./legacy.cjs";

// The untyped JavaScript infers `any`, which satisfies every annotation but
// `never`: these lines compile only when the import resolved to the declaration.
type IsAny<T> = 0 extends 1 & T ? true : false;
type Declared<T> = IsAny<T> extends true ? never : T;

export const esm: Declared<ReturnType<typeof flavour>> = flavour("esm");
export const cjs: Declared<ReturnType<typeof legacyFlavour>> = legacyFlavour("cjs");
