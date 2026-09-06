// @types/node reaches this program only through vite's closure: the forest
// links it, and `types` does not name it, so its globals stay out of scope.
// @ts-expect-error TS2304: Cannot find name 'Buffer'.
export const probe: Buffer | undefined = undefined;
