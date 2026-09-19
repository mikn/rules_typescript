import { expect, it } from "vitest";

declare global {
  // eslint-disable-next-line no-var
  var __compiledSetupBeside: string | undefined;
}

it("ran the compiled sibling of the setup file the config beside it names", () => {
  expect(globalThis.__compiledSetupBeside).toMatch(/\/vitest\.setup\.js$/);
});
