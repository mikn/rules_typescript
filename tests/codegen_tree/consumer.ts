import { greeting } from "#codegen/tree";

export function shout(count: number): string {
    return greeting(count).toUpperCase();
}
