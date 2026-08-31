import type { DevWatchOptions } from "rolldown/experimental";

export function watcherEnabled(options: DevWatchOptions): boolean | undefined {
  return options.enabled;
}
