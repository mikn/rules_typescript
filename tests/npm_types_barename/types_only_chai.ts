import { config } from "chai";

export function truncates(): number {
  return config.truncateThreshold;
}
