/// <reference types="@cloudflare/vitest-pool-workers/types" />
import { SELF } from "cloudflare:test";
import { describe, expect, it } from "vitest";

declare const __UNDECLARED__: string;

import { moduleUrl } from "../src/index";
import wranglerRaw from "../wrangler.jsonc?raw";

describe("nested worker", () => {
  it("runs in workerd with env.test, a bare import and a Text module", async () => {
    expect(navigator.userAgent).toBe("Cloudflare-Workers");
    const res = await SELF.fetch("https://example.com/health");
    expect(res.status).toBe(200);
    const body = (await res.json()) as {
      ok: string;
      greeting: string | null;
      text: string;
      module: string;
    };
    expect(body.ok).toBe("ok");
    expect(body.greeting).toBe("from-env-test");
    expect(body.text).toBe("hello from txt");
  });

  it("serves the one module the test imports", async () => {
    const res = await SELF.fetch("https://example.com/health");
    const body = (await res.json()) as { module: string };
    expect(body.module).toBe(moduleUrl);
    expect(body.module).toMatch(/\/src\/index\.(ts|js)$/);
  });

  it("reads the staged copy through a ?raw import of the wrangler config", () => {
    const extension = moduleUrl.endsWith(".ts") ? "ts" : "js";
    expect(wranglerRaw.match(/"main"\s*:\s*"[^"]*"/g)).toEqual([
      `"main": "src/index.${extension}"`,
      `"main": "src/environment.${extension}"`,
      '"main": "src/not-declared.ts"',
    ]);
    const parsed = JSON.parse(
      wranglerRaw.replace(/^\s*\/\/.*$/gm, "").replace(/,(\s*[}\]])/g, "$1"),
    ) as {
      env: { test: { vars: { GREETING: string } } };
    };
    expect(parsed.env.test.vars.GREETING).toBe("from-env-test");
  });

  it("refuses a build output the runfiles do not hold", async () => {
    // A variable keeps vite's import analysis from resolving it while the file loads.
    const undeclared = __UNDECLARED__ + "?raw";
    await expect(import(undeclared)).rejects.toThrow(/runfiles do not hold/);
  });

  it("404s anything else", async () => {
    const res = await SELF.fetch("https://example.com/nope");
    expect(res.status).toBe(404);
  });
});
