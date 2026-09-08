/// <reference types="@cloudflare/vitest-pool-workers/types" />
import { SELF } from "cloudflare:test";
import { describe, expect, it } from "vitest";

import { routes } from "../src/index";

describe("pooled worker", () => {
  it("runs in workerd", () => {
    expect(navigator.userAgent).toBe("Cloudflare-Workers");
  });

  it("answers every route", async () => {
    for (const route of routes) {
      const res = await SELF.fetch(`https://example.com${route}`);
      expect(res.status).toBe(200);
      expect(await res.text()).toBe("ok");
    }
  });
});
