// leaks.test.ts with @cloudflare/workers-types kept out of the ts_test's own
// program: the same dep, the same `import()`, the same DOM call.
import { expect, test } from "vitest";

export function attach(host: Element, child: HTMLDivElement): void {
  host.append(child);
}

export function later(): void {
  void import("wrangler");
}

test("the compile is the assertion", () => {
  expect(typeof attach).toBe("function");
});
