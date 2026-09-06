import { beforeEach } from "vitest";

beforeEach(() => {
  (globalThis as { __compiledSetupRan?: boolean }).__compiledSetupRan = true;
});
