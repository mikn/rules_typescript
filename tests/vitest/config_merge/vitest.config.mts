// A user-supplied vitest config. ts_test merges it into the config it
// generates, so the plugin below coexists with the Bazel-owned layer.

// The bare `zod` import exercises a config's own npm dependency.
import { z } from "zod";

const answer = z.number().parse(42);

const answerPlugin = {
  name: "rules-ts-user-answer",
  resolveId(id: string): string | null {
    return id === "virtual:answer" ? "\0virtual:answer" : null;
  },
  load(id: string): string | null {
    return id === "\0virtual:answer" ? `export default ${answer};` : null;
  },
};

export default {
  plugins: [answerPlugin],
};
