import { createRequire } from "node:module";
import type { MinimatchOptions } from "minimatch";
import { expect, test } from "vitest";
import { ownerHasBraceExpandMax, sourceOwnerVersion } from "../npm/multi_version/source_owner";

test("a source dependency resolves its own npm version instead of the consumer version", () => {
  const consumerHasBraceExpandMax: "braceExpandMax" extends keyof MinimatchOptions ? true : false = true;
  expect(ownerHasBraceExpandMax).toBe(false);
  expect(consumerHasBraceExpandMax).toBe(true);
  expect(sourceOwnerVersion()).toBe("9.0.9");
  expect(createRequire(import.meta.url)("minimatch/package.json").version).toBe("10.2.4");
});
