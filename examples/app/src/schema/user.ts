import { z } from "zod";

// Annotate with the concrete zod type so oxc can emit the .d.ts without
// cross-file type inference (isolatedDeclarations requirement).
export const UserSchema: z.ZodObject<{
  id: z.ZodString;
  name: z.ZodString;
  email: z.ZodString;
  age: z.ZodNumber;
}> = z.object({
  id: z.string().uuid(),
  name: z.string().min(1),
  email: z.string().email(),
  age: z.number().int().positive(),
});

// Explicit type instead of z.infer<...> — also satisfies isolatedDeclarations.
export type User = {
  id: string;
  name: string;
  email: string;
  age: number;
};

export function validateUser(data: unknown): User {
  return UserSchema.parse(data) as User;
}
