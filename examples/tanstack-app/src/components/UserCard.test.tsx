import { describe, expect, it } from "vitest";

import type { UserCardProps } from "./UserCard";
import { UserCard } from "./UserCard";

describe("UserCardProps", () => {
  it("has the expected shape", () => {
    const props: UserCardProps = {
      id: "550e8400-e29b-41d4-a716-446655440000",
      name: "Alice",
      email: "alice@example.com",
      role: "admin",
    };
    expect(props.name).toBe("Alice");
    expect(props.role).toBe("admin");
  });

  it("accepts all roles", () => {
    const roles: Array<UserCardProps["role"]> = ["admin", "user", "viewer"];
    expect(roles).toHaveLength(3);
  });
});

describe("UserCard function", () => {
  it("is a function", () => {
    expect(typeof UserCard).toBe("function");
  });
});
