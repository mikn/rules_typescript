import {describe, expect, it} from "vitest";

import {handler} from "./handler";

describe("the worker handler", () => {
	it("type-checks against the declarations its project tsconfig names", () => {
		// WORKER_BUILD_ID is declared and never defined, so `typeof` is the
		// one reference to it that both type-checks and runs.
		const env: WorkerEnv = {bucket: "b"};
		expect(env.bucket).toBe("b");
		expect(typeof WORKER_BUILD_ID).toBe("undefined");
		expect(typeof handler).toBe("function");
	});
});
