import { expect, it } from "vitest";

declare const __ANSWER__: number;

it("reads the define plain vitest would read from vite.config.ts", () => {
  expect(__ANSWER__).toBe(42);
});
