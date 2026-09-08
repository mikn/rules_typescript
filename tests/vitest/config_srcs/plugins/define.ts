import { mergeConfig } from "vitest/config";

import meta from "./meta.json";

// A plugin module the config imports relatively, itself importing a bare
// package and a JSON sibling: every route the staged copy has to resolve.
export const defineFromMeta = () => ({
  name: "config-srcs-define",
  config: () =>
    mergeConfig(
      {},
      { define: { __CONFIG_SRCS__: JSON.stringify(meta.value) } },
    ),
});
