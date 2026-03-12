/**
 * Users route component — renders the user list page (/users).
 *
 * Demonstrates using validated search params from Zod schemas.
 */

import type { ReactElement } from "react";
import { UserCard } from "../components";
import type { UserCardProps } from "../components";

// Demo data — in a real app this would come from the route's loader.
const DEMO_USERS: UserCardProps[] = [
  {
    id: "550e8400-e29b-41d4-a716-446655440000",
    name: "Alice Admin",
    email: "alice@example.com",
    role: "admin",
  },
  {
    id: "660e8400-e29b-41d4-a716-446655440001",
    name: "Bob User",
    email: "bob@example.com",
    role: "user",
  },
  {
    id: "770e8400-e29b-41d4-a716-446655440002",
    name: "Carol Viewer",
    email: "carol@example.com",
    role: "viewer",
  },
];

export function UsersComponent(): ReactElement {
  return (
    <div className="page page--users">
      <h1>Users</h1>
      <div className="user-list">
        {DEMO_USERS.map((user) => (
          <UserCard key={user.id} {...user} />
        ))}
      </div>
    </div>
  );
}
