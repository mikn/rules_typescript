// No @types/node on the target that compiles this, so `process` is TS2591.
export const cwd: string = process.cwd();
