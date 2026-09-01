import type AtRule from "postcss/lib/at-rule";
import type { DevWatchOptions } from "rolldown/experimental";

import "vite/client";

export function describe(rule: AtRule, options: DevWatchOptions): string {
  return `${rule.name}:${options.enabled}`;
}
