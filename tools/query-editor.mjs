import { realpathSync } from "node:fs";
import { isAbsolute, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { queryNative, withCommandSignals } from "./verify-editor.mjs";

export async function main(args) {
  return withCommandSignals(async (signal) => {
    if (args.length !== 3)
      throw new Error("usage: query-editor <tsgo-executable> <workspace> <query-json>");
    const [executable, cwd, raw] = args;
    if (!isAbsolute(executable) || !isAbsolute(cwd))
      throw new Error("compiler executable and workspace must be absolute paths");
    if (raw === "--module-path") {
      process.stdout.write(
        realpathSync(fileURLToPath(new URL("./verify-editor.mjs", import.meta.url))) + "\n",
      );
      return;
    }
    const query = JSON.parse(raw);
    const result = await queryNative({ executable, cwd: resolve(cwd), signal }, query);
    process.stdout.write(JSON.stringify(result) + "\n");
  });
}

if (
  process.argv[1] &&
  realpathSync(fileURLToPath(import.meta.url)) === realpathSync(resolve(process.argv[1]))
) {
  try {
    await main(process.argv.slice(2));
  } catch (error) {
    process.stderr.write(String(error) + "\n");
    process.exitCode = 1;
  }
}
