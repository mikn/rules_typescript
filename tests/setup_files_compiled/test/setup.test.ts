import { expect, inject, it } from "vitest";

declare global {
  // eslint-disable-next-line no-var
  var __compiledSetupRan: string | undefined;
}

declare module "vitest" {
  interface ProvidedContext {
    setupFilesCompiled: string;
  }
}

it("ran the compiled sibling of the setup file the config names", () => {
  expect(globalThis.__compiledSetupRan).toMatch(/\/test\/vitest\.setup\.js$/);
});

it("ran the compiled sibling of the global setup file the config names", () => {
  expect(inject("setupFilesCompiled")).toMatch(/\/test\/global-setup\.js$/);
});
