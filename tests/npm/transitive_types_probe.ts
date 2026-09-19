// @types/node reaches this program only through vite's closure, not `types`.
// @ts-expect-error TS2304: Cannot find name 'Buffer'.
export const probe: Buffer | undefined = undefined;
