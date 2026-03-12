/**
 * User validation schema using Zod.
 *
 * Demonstrates npm dependency usage (zod) with explicit type annotations
 * required by isolated declarations mode.
 */

import { z } from "zod";

// Explicit Zod type annotation satisfies isolatedDeclarations — oxc can emit
// the .d.ts without needing to infer the type across file boundaries.
export const UserSchema: z.ZodObject<{
  id: z.ZodString;
  name: z.ZodString;
  email: z.ZodString;
  role: z.ZodEnum<["admin", "user", "viewer"]>;
}> = z.object({
  id: z.string().uuid(),
  name: z.string().min(1).max(100),
  email: z.string().email(),
  role: z.enum(["admin", "user", "viewer"]),
});

// Explicit type definition (not z.infer) also satisfies isolatedDeclarations.
export type User = {
  id: string;
  name: string;
  email: string;
  role: "admin" | "user" | "viewer";
};

export const CreateUserSchema: z.ZodObject<{
  name: z.ZodString;
  email: z.ZodString;
  role: z.ZodDefault<z.ZodEnum<["admin", "user", "viewer"]>>;
}> = z.object({
  name: z.string().min(1).max(100),
  email: z.string().email(),
  role: z.enum(["admin", "user", "viewer"]).default("user"),
});

export type CreateUserInput = {
  name: string;
  email: string;
  role?: "admin" | "user" | "viewer";
};

export function validateUser(data: unknown): User {
  return UserSchema.parse(data) as User;
}

export function validateCreateUser(data: unknown): CreateUserInput {
  return CreateUserSchema.parse(data) as CreateUserInput;
}
