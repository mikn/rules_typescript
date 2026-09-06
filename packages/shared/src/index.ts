// The workspace member tests/npm/pnpm-lock.yaml links as `shared`. Its exports
// map names this file and src/wire/index.ts; zod is its own lockfile dep. The
// directory import is the member's own shape: node's loader cannot follow it.
import { z } from "zod";

export { frame } from "./wire";

const Name = z.string().min(1);

export function greet(name: string): string {
    return `Hello, ${Name.parse(name)}!`;
}
