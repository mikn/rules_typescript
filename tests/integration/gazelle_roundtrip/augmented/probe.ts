import type { ViteUserConfig } from "vitest/config";

// vite is never imported here: the augmentation alone names it.
declare module "vite" {
  interface UserConfig {
    probe?: string;
  }
}

export const config: ViteUserConfig = { probe: "augmented" };
