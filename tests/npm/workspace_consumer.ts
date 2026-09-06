// @npm//:shared is the hub's view of //packages/shared:shared, linked into the
// forest at node_modules/shared with the member's package.json as built: the
// bare name and the exports subpath resolve through that manifest.
import { greet } from "shared";
import { frame } from "shared/wire";

export const message: string = frame(greet("World"));
