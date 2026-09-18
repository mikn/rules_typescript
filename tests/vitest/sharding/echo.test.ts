import { it } from "vitest";

import { expectOwnShard } from "./shard";

it("echo.test.ts runs in its own shard, staged alone", () => {
  expectOwnShard(import.meta.url);
});
