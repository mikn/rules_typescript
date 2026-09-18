import { it } from "vitest";

import { expectOwnShard } from "./shard";

it("bravo.test.ts runs in its own shard, staged alone", () => {
  expectOwnShard(import.meta.url);
});
