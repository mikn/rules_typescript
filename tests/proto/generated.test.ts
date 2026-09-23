import { create, fromJson, toJson } from "@bufbuild/protobuf";
import { expect, test } from "vitest";

import { GreetingSchema, type GreetingJson } from "./generated/proof/v1/message_pb";

test("generated sibling source preserves protobuf JSON int64 and optional presence", () => {
  const json: GreetingJson = {
    text: "hello",
    count: "9007199254740993",
    detail: { value: "shared" },
  };
  expect(toJson(GreetingSchema, fromJson(GreetingSchema, json))).toEqual(json);
  expect(toJson(GreetingSchema, create(GreetingSchema, { text: "hello" }))).toEqual({
    text: "hello",
  });
});
