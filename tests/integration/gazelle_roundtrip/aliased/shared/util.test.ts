import {describe, expect, it} from "vitest";

import type {Greeting} from "#shared/util";
import {greet} from "./util";

describe("a test with a src under the alias directory", () => {
	it("resolves #shared/util without staging anything beyond its deps", () => {
		const g: Greeting = greet("bazel");
		expect(g).toBe("hello bazel");
	});
});
