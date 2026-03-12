// Demonstrates typed JSON import: config.name is string, config.port is number.
import config from "./config.json";

export function getAppName(): string {
  // TypeScript knows config.name is a string.
  return config.name;
}

export function getPort(): number {
  // TypeScript knows config.port is a number.
  return config.port;
}

export function isDebug(): boolean {
  // TypeScript knows config.debug is a boolean.
  return config.debug;
}

export function getDbHost(): string {
  // TypeScript knows config.database.host is a string (nested object).
  return config.database.host;
}
