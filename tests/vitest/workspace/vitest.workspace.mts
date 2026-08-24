// A vitest workspace definition: an array of projects. ts_test reads an array
// default-export as test.workspace and re-applies the Bazel-owned config to
// every project, since each project gets its own Vite server.
export default [
  {
    test: {
      name: "alpha",
    },
  },
];
