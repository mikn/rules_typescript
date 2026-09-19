import {describe, expect, it} from "vitest";

import {name} from "shared";

describe("the member imported by its own name", () => {
	it("reaches the entry the exports map designates", () => {
		expect(name).toBe("shared");
	});
});
