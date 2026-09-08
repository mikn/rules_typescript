import { beforeEach } from "vitest";

beforeEach(() => {
  (globalThis as { __compiledSetupDom?: boolean }).__compiledSetupDom = true;
});
