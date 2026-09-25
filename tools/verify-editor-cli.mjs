import { realpathSync } from "node:fs";
import { resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { verify, withCommandSignals } from "./verify-editor.mjs";

if (
  process.argv[1] &&
  realpathSync(fileURLToPath(import.meta.url)) === realpathSync(resolve(process.argv[1]))
) {
  if (process.env.BUILD_WORKSPACE_DIRECTORY) process.chdir(process.env.BUILD_WORKSPACE_DIRECTORY);
  const [, , tsgo, probesFile] = process.argv;
  if (!tsgo || !probesFile)
    throw new Error("usage: node verify-editor.mjs <tsgo-executable> <probes.json>");
  await withCommandSignals((signal) => verify(tsgo, probesFile, { signal }));
}
