// The member tests/npm/pnpm-lock.yaml links as `shared`. The directory import
// is the member's own shape: node's loader cannot follow it.
import { z } from "zod";

import banner from "./banner.json";

export { frame } from "./wire";

export const tagline: string = banner.tagline;

const Name = z.string().min(1);

export function greet(name: string): string {
    return `Hello, ${Name.parse(name)}!`;
}
