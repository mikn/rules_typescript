import { test } from "node:test";

import { assertOwnShard } from "./shard";

test("delta.test.ts runs in its own shard", () => {
  assertOwnShard(import.meta.url);
});
