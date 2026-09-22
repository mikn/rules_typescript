import { defineConfig } from "vitest/config";

import { defineFromMeta } from "./plugins/define";

export default defineConfig({ plugins: [defineFromMeta()] });
