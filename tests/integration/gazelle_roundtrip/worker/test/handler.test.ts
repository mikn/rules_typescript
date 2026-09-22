import {describe, expect, it} from "vitest";

import {handler} from "../src/handler";

describe("the worker handler, from a test directory with a tsconfig of its own", () => {
	it("type-checks against the declaration its tsconfig names one directory up", () => {
		const env: WorkerEnv = {bucket: "b"};
		expect(env.bucket).toBe("b");
		expect(typeof WORKER_BUILD_ID).toBe("undefined");
		expect(typeof handler).toBe("function");
	});
});
