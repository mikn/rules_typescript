import { z } from "zod";

export const schema = z.object({ size: z.enum(["sm", "md"]) });

export function accepts(input: unknown): boolean {
  return schema.safeParse(input).success;
}
