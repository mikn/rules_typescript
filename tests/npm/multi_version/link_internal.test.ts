import { expect, test } from "vitest";

import { escape } from "./node_modules/minimatch/dist/esm/escape.js";

test("an unexposed module loads through the link", () => {
  expect(escape("a*b")).toBe("a\\*b");
});
