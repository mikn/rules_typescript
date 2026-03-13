import { z } from "zod";

// Explicit interface — no inference from schema, compatible with isolated declarations.
export interface User {
  id: number;
  name: string;
  email: string;
}

const userSchema = z.object({
  id: z.number(),
  name: z.string(),
  email: z.string().email(),
});

export function parseUser(input: unknown): User {
  return userSchema.parse(input) as User;
}
