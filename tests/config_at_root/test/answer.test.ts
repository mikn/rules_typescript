import { expect, it } from "vitest";

declare global {
  // eslint-disable-next-line no-var
  var __answer: number | undefined;
}

it("saw the setup file the root config names", () => {
  expect(globalThis.__answer).toBe(42);
});
