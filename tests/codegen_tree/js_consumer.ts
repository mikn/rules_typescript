import { greeting } from "#codegen/tree";
import { suffix } from "./helper.js";

export function greet(count: number): string {
    return greeting(count) + suffix;
}
