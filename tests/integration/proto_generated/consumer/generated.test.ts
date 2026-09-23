import { create, fromJson, toJson } from "@bufbuild/protobuf";
import { expect, test } from "vitest";
import { MessageSchema } from "../schema/generated/plain/messages/message_pb";
import { MessageSchema as JsonSchema, type MessageJson } from "../schema/generated/json/messages/message_pb";

test("generated identities preserve sibling and timestamp imports at runtime", () => {
  const value: MessageJson = { common: { value: "shared" }, at: "2026-01-01T00:00:00Z" };
  expect(toJson(JsonSchema, fromJson(JsonSchema, value))).toEqual(value);
  expect(toJson(MessageSchema, create(MessageSchema, { common: { value: "shared" } }))).toEqual({ common: { value: "shared" } });
});
