import {describe, expect, it} from "vitest";

import {handler} from "../../src/handler";

describe("the worker handler, from a directory whose tsconfig names the declaration two up", () => {
	it("type-checks against the declaration past the tsconfig between", () => {
		const env: WorkerEnv = {bucket: "b"};
		expect(env.bucket).toBe("b");
		expect(typeof WORKER_BUILD_ID).toBe("undefined");
		expect(typeof handler).toBe("function");
	});
});
