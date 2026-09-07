// @npm//:shared is the hub's view of //packages/shared:shared, linked at
// node_modules/shared with the member's package.json as built.
import { greet } from "shared";
import { frame } from "shared/wire";

export const message: string = frame(greet("World"));
