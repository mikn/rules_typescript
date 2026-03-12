import { describe, expect, it } from "vitest";

import { validateUser } from "./user";

describe("UserSchema", () => {
  it("validates a correct user", () => {
    const user = validateUser({
      id: "550e8400-e29b-41d4-a716-446655440000",
      name: "Alice",
      email: "alice@example.com",
      age: 30,
    });
    expect(user.name).toBe("Alice");
  });

  it("rejects invalid email", () => {
    expect(() =>
      validateUser({
        id: "550e8400-e29b-41d4-a716-446655440000",
        name: "Bob",
        email: "not-an-email",
        age: 25,
      }),
    ).toThrow();
  });

  it("rejects negative age", () => {
    expect(() =>
      validateUser({
        id: "550e8400-e29b-41d4-a716-446655440000",
        name: "Charlie",
        email: "charlie@example.com",
        age: -5,
      }),
    ).toThrow();
  });
});
