import { beforeAll } from "vitest";

beforeAll(() => {
  (globalThis as { __commonjsSetup?: string }).__commonjsSetup = "ran";
});
