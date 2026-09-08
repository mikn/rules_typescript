import { expect, it } from "vitest";

it("runs although the config's include names the sources alone", () => {
  expect(import.meta.url.endsWith("/include.test.js")).toBe(true);
});
