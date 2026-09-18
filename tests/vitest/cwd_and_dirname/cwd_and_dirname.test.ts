import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { expect, it } from "vitest";

declare const __CONFIG_DIRNAME__: string;
declare const __CONFIG_FILENAME__: string;
declare const __CONFIG_META_DIRNAME__: string;
declare const __PLUGIN_DIRNAME__: string;

const here = dirname(fileURLToPath(import.meta.url));

it("runs from the package directory", () => {
  expect(process.cwd()).toBe(here);
  const text = readFileSync(join(process.cwd(), "fixtures/x.txt"), "utf8");
  expect(text.trim()).toBe("read from the package directory");
});

it("loads the config from the package directory", () => {
  expect(__CONFIG_DIRNAME__).toBe(here);
  expect(__CONFIG_META_DIRNAME__).toBe(here);
  expect(__CONFIG_FILENAME__).toBe(join(here, "vitest.config.mts"));
  expect(__PLUGIN_DIRNAME__).toBe(join(here, "plugins"));
});
