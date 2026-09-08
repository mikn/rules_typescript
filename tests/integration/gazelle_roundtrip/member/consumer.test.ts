import {describe, expect, it} from "vitest";

import {name} from "shared";
import {frame} from "shared/wire";

describe("a member imported by name from another package", () => {
	it("resolves the name and the exports subpath through the hub's view", () => {
		expect(frame(name)).toBe("[shared]");
	});
});
