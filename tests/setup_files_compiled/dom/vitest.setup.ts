import { beforeEach } from "vitest";

beforeEach(() => {
  (globalThis as { __compiledSetupDom?: string }).__compiledSetupDom =
    import.meta.url;
});
