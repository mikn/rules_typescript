/**
 * Shared utility for creating typed ESLint rules via @typescript-eslint/utils.
 *
 * Centralising the import here means individual rule files do not need to
 * repeat the verbose import path and generic parameters.
 */

import { ESLintUtils } from "@typescript-eslint/utils";

export const createRule = ESLintUtils.RuleCreator(
  (name) =>
    `https://github.com/mikn/rules_typescript/tree/main/tools/isolated-declarations-lint/docs/rules/${name}.md`
);
