import data from "./shapes.json";

// Sampling the first element erased `b`, so this read was an error.
export const maybeB: number | undefined = data.xs[1].b;

// A JSON module is not readonly, so this assignment is the one the repo's own
// `resolveJsonModule` build makes.
export const mutable: { a: number }[] = data.xs;

export const deep: number | undefined = data.nested[0].o.q;
export const nothing: never[] = data.empty;
export const scalars: (string | number | boolean)[] = data.mixed;
export const nullable: ({ a: number } | null)[] = data.nullable;
export const nothingInside: object = data.obj;
