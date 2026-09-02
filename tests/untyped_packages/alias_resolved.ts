// `ms` ships no declarations of its own; @types/ms ships them, and the bare
// name resolves to that directory rather than to ms itself. Excluding `ms` has
// to take this key away too, or the attribute is a no-op for every package
// typed the way DefinitelyTyped types this one.
import ms from "ms";

export const day: number = ms("1 day");
