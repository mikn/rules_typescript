import { describe, expect, it } from "vitest";
import { isValidEmail, parseUser } from "./user";

describe("parseUser", () => {
  it("parses a valid user object", () => {
    const result = parseUser({ id: 1, name: "Alice", email: "alice@example.com" });
    expect(result.id).toBe(1);
    expect(result.name).toBe("Alice");
  });

  it("throws on invalid input", () => {
    expect(() => parseUser({ id: "not-a-number", name: "Bob", email: "bad" })).toThrow();
  });
});

describe("isValidEmail", () => {
  it("accepts a valid email", () => {
    expect(isValidEmail("alice@example.com")).toBe(true);
  });

  it("rejects an invalid email", () => {
    expect(isValidEmail("not-an-email")).toBe(false);
  });
});
