import { expect, it } from "vitest";

declare const __CONFIG_SRCS__: string;

it("runs under the plugin a module staged beside the config defined", () => {
  expect(__CONFIG_SRCS__).toBe("staged beside the config");
});
