// `process` is @types/node's global. Nothing here imports the package, no
// tsconfig lists it, and env.d.ts's directive is the one place it is named.
export const cwd: string = process.cwd();
