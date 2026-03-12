import { describe, expect, it } from "vitest";

import {
  validateUsersSearch,
  validateUserParams,
} from "./params";
import type { UsersSearch, UserParams } from "./params";

const VALID_UUID = "550e8400-e29b-41d4-a716-446655440000";

describe("validateUsersSearch", () => {
  it("applies default values when fields are omitted", () => {
    const result = validateUsersSearch({});
    const typed: UsersSearch = result;
    expect(typed.page).toBe(1);
    expect(typed.limit).toBe(20);
    expect(typed.filter).toBe("");
  });

  it("accepts explicit values", () => {
    const result = validateUsersSearch({
      page: 2,
      limit: 50,
      filter: "alice",
    });
    expect(result.page).toBe(2);
    expect(result.limit).toBe(50);
    expect(result.filter).toBe("alice");
  });

  it("rejects a limit above 100", () => {
    expect(() => validateUsersSearch({ limit: 200 })).toThrow();
  });

  it("rejects a negative page number", () => {
    expect(() => validateUsersSearch({ page: -1 })).toThrow();
  });
});

describe("validateUserParams", () => {
  it("accepts a valid UUID", () => {
    const result = validateUserParams({ userId: VALID_UUID });
    const typed: UserParams = result;
    expect(typed.userId).toBe(VALID_UUID);
  });

  it("rejects a non-UUID string", () => {
    expect(() => validateUserParams({ userId: "not-a-uuid" })).toThrow();
  });

  it("rejects missing userId", () => {
    expect(() => validateUserParams({})).toThrow();
  });
});
