import { z } from "zod";

// Explicit interface — no inference from schema, fully isolated-declarations
// compatible (exported type has no dependency on unexported const).
export interface User {
  id: number;
  name: string;
  email: string;
}

// Internal schema — NOT exported, so isolated-declarations does not require
// an explicit type annotation on this const.
const userSchema = z.object({
  id: z.number(),
  name: z.string(),
  email: z.string().email(),
});

export function parseUser(input: unknown): User {
  return userSchema.parse(input) as User;
}

export function isValidEmail(email: string): boolean {
  return z.string().email().safeParse(email).success;
}
