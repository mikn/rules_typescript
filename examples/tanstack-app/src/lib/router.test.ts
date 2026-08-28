import { describe, expect, it } from "vitest";

import { getRouter } from "./router";
import type { AppRouter } from "./router";

describe("getRouter", () => {
  it("builds a router from the generated route tree", () => {
    const router: AppRouter = getRouter();
    expect(typeof router.navigate).toBe("function");
  });

  it("builds a fresh router per call, as SSR needs", () => {
    expect(getRouter()).not.toBe(getRouter());
  });

  it("knows every file route", () => {
    const ids = Object.keys(getRouter().routesById);
    expect(ids).toContain("/");
    expect(ids).toContain("/about");
    expect(ids).toContain("/users");
  });
});
