// A user-supplied vitest config. ts_test merges it into the config it
// generates, so the plugin below coexists with the Bazel-owned CSS-module mock.
const answerPlugin = {
  name: "rules-ts-user-answer",
  resolveId(id: string): string | null {
    return id === "virtual:answer" ? "\0virtual:answer" : null;
  },
  load(id: string): string | null {
    return id === "\0virtual:answer" ? "export default 42;" : null;
  },
};

export default {
  plugins: [answerPlugin],
};
