import { expect, it } from "vitest";

declare global {
  // eslint-disable-next-line no-var
  var __compiledSetupDom: boolean | undefined;
}

it("ran the compiled sibling of the setup file under a DOM environment", () => {
  expect(document.body).toBeDefined();
  expect(globalThis.__compiledSetupDom).toBe(true);
});
