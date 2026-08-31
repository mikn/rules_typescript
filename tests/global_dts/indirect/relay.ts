import { handler } from "../consumer/handler";

export function relay(env: Env): string {
  return handler(env);
}
