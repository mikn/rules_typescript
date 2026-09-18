import assert from "node:assert/strict";
import { basename } from "node:path";
import { fileURLToPath } from "node:url";

const FILES = [
  "alpha.test.js",
  "bravo.test.js",
  "charlie.test.js",
  "delta.test.js",
  "echo.test.js",
  "foxtrot.test.js",
];

export function shardFiles(index: number, total: number): string[] {
  return FILES.filter((_, i) => i % total === index);
}

export function assertOwnShard(url: string): void {
  const total = Number(process.env.TEST_TOTAL_SHARDS);
  const index = Number(process.env.TEST_SHARD_INDEX);
  assert.equal(total, 3);
  const own = shardFiles(index, total);
  const file = basename(fileURLToPath(url));
  assert.ok(own.includes(file), `${file} ran in shard ${index}, ${own}`);
}
