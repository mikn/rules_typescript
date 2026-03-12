import type { User } from "../schema";
import { validateUser } from "../schema";

export function processUser(rawData: unknown): string {
  const user: User = validateUser(rawData);
  return `User ${user.name} (${user.email}), age ${user.age}`;
}
