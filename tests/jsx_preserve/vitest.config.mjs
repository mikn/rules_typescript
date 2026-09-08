import { resolve } from "node:path";

// The JSX runtime is the package under test; the alias is what node_modules
// would be for a published one.
export default {
  oxc: {
    jsx: {
      runtime: "automatic",
      importSource: "@acme/jsx",
      development: false,
    },
  },
  resolve: { alias: { "@acme/jsx": resolve(process.env.TS_TEST_PACKAGE_DIR) } },
};
