// Shared workspace package — used to test pnpm workspace:* alias generation.
// The @npm//:shared alias (generated from link:packages/shared in pnpm-lock.yaml)
// points to //packages/shared:shared.
import { z } from "zod";

const Name = z.string().min(1);

export function greet(name: string): string {
    return `Hello, ${Name.parse(name)}!`;
}
