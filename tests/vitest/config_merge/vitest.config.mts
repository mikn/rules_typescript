// A user-supplied vitest config. ts_test merges it into the config it
// generates, so the plugin below coexists with the Bazel-owned CSS-module mock.
//
// The bare `zod` import is here so that a config's own npm dependency is
// exercised at all: a config is loaded before any test runs, from a staged copy,
// and nothing else in the suite imports a package from one. It does NOT pin
// where that copy is staged -- this config resolves zod either way, and the case
// that does not is a `.ts` config importing a package whose own entry point is
// ESM-only. See TODO.md on the Workers pool.
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
