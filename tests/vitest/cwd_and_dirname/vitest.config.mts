import { defineConfig } from "vitest/config";

import { where } from "./plugins/where";

export default defineConfig({
  plugins: [
    where({
      dirname: __dirname,
      filename: __filename,
      metaDirname: import.meta.dirname,
    }),
  ],
});
