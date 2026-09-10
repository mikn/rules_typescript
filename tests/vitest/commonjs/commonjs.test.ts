import { expect, it } from "vitest";

import { answer } from "./helper";

declare global {
  // eslint-disable-next-line no-var
  var __commonjsSetup: string | undefined;
}

it("runs the setup and the helper as the ES modules vitest imports", () => {
  expect(globalThis.__commonjsSetup).toBe("ran");
  expect(answer()).toBe(42);
});
