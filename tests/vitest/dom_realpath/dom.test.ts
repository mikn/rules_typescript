import { greet } from "shared";
import { expect, it } from "vitest";
import { z } from "zod";

import { add } from "../math.js";
import fixture from "./fixture.json";

it("runs under a DOM environment", () => {
  expect(document.body).toBeDefined();
});

it("keeps the test at its runfiles path", () => {
  expect(import.meta.dirname).toBe(process.cwd());
});

it("loads another package's compiled module", () => {
  expect(add(1, 2)).toBe(3);
});

it("loads a source file the runfiles hold", () => {
  expect(fixture.answer).toBe(42);
});

it("loads a workspace member vite inlines at its realpath", () => {
  expect(greet("World")).toBe("Hello, World!");
});

it("loads an npm package", () => {
  expect(z.string().parse("x")).toBe("x");
});
