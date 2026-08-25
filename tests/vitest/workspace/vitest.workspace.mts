// An array default-export: a list of vitest projects. ts_test reads it as
// test.projects and re-applies the Bazel-owned config to every entry, since
// each project gets its own Vite server.
export default [
  {
    test: {
      name: "alpha",
    },
  },
];
