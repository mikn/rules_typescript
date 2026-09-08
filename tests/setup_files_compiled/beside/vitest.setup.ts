import { beforeEach } from "vitest";

beforeEach(() => {
  (globalThis as { __compiledSetupBeside?: string }).__compiledSetupBeside =
    import.meta.url;
});
