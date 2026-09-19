import { it } from "vitest";

import { expectOwnShard } from "./shard";

it("alpha.test.ts runs in its own shard, staged alone", () => {
  expectOwnShard(import.meta.url);
});
