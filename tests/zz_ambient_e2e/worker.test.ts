import { expect, test } from "vitest";
import { handler } from "./worker";

test("ambient globals are in the test program", () => {
	const env: ZzEnv = { KV: "kv" };
	globalThis.__ZZ_AMBIENT__ = "!";
	expect(handler(env)).toBe("kv!");
});
