import { z } from "zod";

// Use zod at runtime — the type is not inferred in module position.
export function parseUser(input: unknown): { name: string; age: number } {
  const schema = z.object({
    name: z.string(),
    age: z.number(),
  });
  return schema.parse(input) as { name: string; age: number };
}
