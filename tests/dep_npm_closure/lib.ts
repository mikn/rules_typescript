import { z } from "zod";

export const schema = z.object({ size: z.enum(["sm", "md"]) });

export interface Props extends z.infer<typeof schema> {
  children: string;
}

export function onParse(callback: (issue: z.ZodIssue) => void): void {
  const result = schema.safeParse({});
  if (!result.success) {
    for (const issue of result.error.issues) {
      callback(issue);
    }
  }
}
