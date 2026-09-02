// The same import with `ms` excluded: no key resolves the bare name, and this
// target's own `declare module` is what says what the import means.
import ms from "ms";

export const day: number = ms("1 day");
