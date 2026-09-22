import {describe, expect, it} from "vitest";

import answer from "virtual:answer";

describe("a test one package below the vitest config", () => {
	it("runs under the plugin that config installs", () => {
		expect(answer).toBe(42);
	});
});
