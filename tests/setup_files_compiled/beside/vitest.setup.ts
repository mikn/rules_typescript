import { beforeEach } from "vitest";

beforeEach(() => {
  (globalThis as { __compiledSetupBeside?: boolean }).__compiledSetupBeside = true;
});
