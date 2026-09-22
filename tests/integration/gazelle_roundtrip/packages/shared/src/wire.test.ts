import {describe, expect, it} from "vitest";

import {frame} from "shared/wire";

describe("the member imported through an exports subpath", () => {
	it("reaches the file the subpath designates", () => {
		expect(frame("x")).toBe("[x]");
	});
});
