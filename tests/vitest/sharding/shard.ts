import { readdirSync } from "node:fs";
import { basename } from "node:path";
import { fileURLToPath } from "node:url";
import { expect } from "vitest";

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

export function expectOwnShard(url: string): void {
  const total = Number(process.env.TEST_TOTAL_SHARDS);
  const index = Number(process.env.TEST_SHARD_INDEX);
  expect(total).toBe(3);
  const own = shardFiles(index, total);
  expect(own).toContain(basename(fileURLToPath(url)));
  const root = process.env.TS_TEST_FILES_ROOT ?? "";
  const staged = readdirSync(root, { recursive: true, encoding: "utf8" })
    .filter((p) => p.endsWith(".test.js"))
    .map((p) => basename(p))
    .sort();
  expect(staged).toEqual(own);
}
