// The member the root does not link, through this importer's link; the hoist
// holds its name at the root by name alone.
import { greet } from "hoisted-member";

export const greeting: string = greet("World");
