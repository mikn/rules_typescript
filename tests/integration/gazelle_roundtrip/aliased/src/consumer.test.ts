import {describe, expect, it} from "vitest";

import type {Greeting} from "#shared/util";

describe("a test with no src under the alias directory", () => {
	it("type-checks #shared/util against declarations //aliased stages", () => {
		const g: Greeting = "hello";
		expect(g).toBe("hello");
	});
});
