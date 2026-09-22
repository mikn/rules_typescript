// @vitest-environment node
import type { TestAPI } from "vitest";

import { expect, it } from "vitest";

const test: TestAPI = it;

test("runs under the docblock's environment, not the config's", () => {
  expect("window" in globalThis).toBe(false);
});
