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
  resolve: { alias: { "@acme/jsx": import.meta.dirname } },
};
