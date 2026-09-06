import {describe, expect, it} from "vitest";

import type {Greeting} from "#shared/util";

describe("a test with no src under the alias directory", () => {
	it("type-checks #shared/util against the declarations the dep on //aliased/shared stages", () => {
		const g: Greeting = "hello";
		expect(g).toBe("hello");
	});
});
