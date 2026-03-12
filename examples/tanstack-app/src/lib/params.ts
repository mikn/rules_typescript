/**
 * Route parameter validation using Zod.
 *
 * TanStack Router supports Zod for validating search params and path params.
 * This module defines the validation schemas used across routes.
 *
 * All exports carry explicit type annotations for isolated declarations.
 */

import { z } from "zod";

// Search params for the users list page.
export const UsersSearchSchema: z.ZodObject<{
  page: z.ZodDefault<z.ZodNumber>;
  limit: z.ZodDefault<z.ZodNumber>;
  filter: z.ZodDefault<z.ZodString>;
}> = z.object({
  page: z.number().int().positive().default(1),
  limit: z.number().int().min(1).max(100).default(20),
  filter: z.string().default(""),
});

export type UsersSearch = {
  page: number;
  limit: number;
  filter: string;
};

// Path params for the user detail page.
export const UserParamsSchema: z.ZodObject<{
  userId: z.ZodString;
}> = z.object({
  userId: z.string().uuid(),
});

export type UserParams = {
  userId: string;
};

export function validateUsersSearch(data: unknown): UsersSearch {
  return UsersSearchSchema.parse(data) as UsersSearch;
}

export function validateUserParams(data: unknown): UserParams {
  return UserParamsSchema.parse(data) as UserParams;
}
