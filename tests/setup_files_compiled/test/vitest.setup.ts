import { beforeEach } from "vitest";

beforeEach(() => {
  (globalThis as { __compiledSetupRan?: string }).__compiledSetupRan =
    import.meta.url;
});
