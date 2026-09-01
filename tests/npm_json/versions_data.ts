import versions from "electron-to-chromium/versions.json";

export function chromiumFor(electron: string): string | undefined {
  return (versions as Record<string, string>)[electron];
}
