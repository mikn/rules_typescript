import { createFileRoute } from "@tanstack/react-router";
import { z } from "zod";
import { UserCard } from "../components";
import type { UserCardProps } from "../components";

const UsersSearch = z.object({
  page: z.number().int().positive().default(1),
  limit: z.number().int().min(1).max(100).default(20),
});

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
];

function UsersComponent() {
  const { page, limit } = Route.useSearch();
  return (
    <div className="page page--users">
      <h1>tanstack-users-route-marker</h1>
      <p>
        page {page} of {limit}
      </p>
      <div className="user-list">
        {DEMO_USERS.map((user) => (
          <UserCard key={user.id} {...user} />
        ))}
      </div>
    </div>
  );
}

export const Route = createFileRoute("/users")({
  validateSearch: (input: Record<string, unknown>) => UsersSearch.parse(input),
  component: UsersComponent,
});
