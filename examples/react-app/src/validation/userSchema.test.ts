import { describe, expect, it } from "vitest";

import { validateUser, validateCreateUser } from "./userSchema";
import type { User, CreateUserInput } from "./userSchema";

const VALID_UUID = "550e8400-e29b-41d4-a716-446655440000";

describe("validateUser", () => {
  it("parses a valid admin user", () => {
    const user = validateUser({
      id: VALID_UUID,
      name: "Alice Admin",
      email: "alice@example.com",
      role: "admin",
    });
    const typed: User = user;
    expect(typed.name).toBe("Alice Admin");
    expect(typed.role).toBe("admin");
  });

  it("parses a valid viewer", () => {
    const user = validateUser({
      id: VALID_UUID,
      name: "Bob Viewer",
      email: "bob@example.com",
      role: "viewer",
    });
    expect(user.role).toBe("viewer");
  });

  it("rejects an invalid email", () => {
    expect(() =>
      validateUser({
        id: VALID_UUID,
        name: "Charlie",
        email: "not-an-email",
        role: "user",
      }),
    ).toThrow();
  });

  it("rejects an invalid UUID", () => {
    expect(() =>
      validateUser({
        id: "not-a-uuid",
        name: "Dave",
        email: "dave@example.com",
        role: "user",
      }),
    ).toThrow();
  });

  it("rejects an invalid role", () => {
    expect(() =>
      validateUser({
        id: VALID_UUID,
        name: "Eve",
        email: "eve@example.com",
        role: "superuser",
      }),
    ).toThrow();
  });
});

describe("validateCreateUser", () => {
  it("applies default role when omitted", () => {
    const input = validateCreateUser({
      name: "Frank",
      email: "frank@example.com",
    });
    const typed: CreateUserInput = input;
    expect(typed.name).toBe("Frank");
  });

  it("rejects an empty name", () => {
    expect(() =>
      validateCreateUser({
        name: "",
        email: "grace@example.com",
      }),
    ).toThrow();
  });
});
