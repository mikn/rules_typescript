import { expect, it } from "vitest";

declare global {
  // eslint-disable-next-line no-var
  var __compiledSetupBeside: boolean | undefined;
}

it("ran the compiled sibling of the setup file the config beside it names", () => {
  expect(globalThis.__compiledSetupBeside).toBe(true);
});
