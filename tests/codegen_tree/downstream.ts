import { shout } from "./consumer.js";
import { farewell } from "#codegen/tree";

export function both(count: number): string {
    return shout(count) + farewell(count);
}
