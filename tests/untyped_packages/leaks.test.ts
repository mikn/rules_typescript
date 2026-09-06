// leaks.ts on the program ts_test generates: the same wrangler dep, the same
// `import()`, the same DOM call. Manual, since its compile is the failure.
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
