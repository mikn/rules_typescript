import { describe, expect, it } from "vitest";

import { router } from "./router";
import type { AppRouter } from "./router";

describe("router", () => {
  it("is created", () => {
    expect(router).toBeDefined();
  });

  it("satisfies AppRouter interface", () => {
    // Type-level check: router is assignable to AppRouter.
    const typed: AppRouter = router;
    expect(typed).toBeDefined();
  });

  it("has navigate function", () => {
    expect(typeof router.navigate).toBe("function");
  });

  it("has a state", () => {
    expect(router.state).toBeDefined();
  });
});
