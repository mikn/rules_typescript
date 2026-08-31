import type AtRule from "postcss/lib/at-rule";

export function atRuleName(rule: AtRule): string {
  return rule.name;
}
