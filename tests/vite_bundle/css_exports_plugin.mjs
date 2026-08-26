// Dumps the class-name map postcss-modules actually produced, so a test can
// compare it against the .d.ts css_module generated from the same file. The
// hook is set from a plugin because ts_bundle reads only `plugins` out of a
// vite_config.
import { writeFileSync } from "node:fs";

const exportsByFile = {};

export default {
  plugins: [
    {
      name: "dump-css-module-exports",
      config() {
        return {
          css: {
            modules: {
              getJSON(cssFileName, json) {
                exportsByFile[cssFileName.split("/").pop()] = json;
              },
            },
          },
        };
      },
      writeBundle() {
        writeFileSync(
          process.env["VITE_OUT_DIR"] + "/css-module-exports.json",
          JSON.stringify(exportsByFile, null, 2) + "\n",
        );
      },
    },
  ],
};
