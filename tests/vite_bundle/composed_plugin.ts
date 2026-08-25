/**
 * The module composed_config.ts imports relatively, and the extensionless
 * specifier it is imported by is the point: that is how a bundler-resolution
 * config is written, and only Vite's own config loader resolves it.
 */

import type { Plugin } from 'vite';

export function composedPlugin(): Plugin {
  return {
    name: 'rules-typescript-composed-plugin',
    renderChunk(code: string) {
      return 'const _COMPOSED_TS_CONFIG_LOADED = true;\n' + code;
    },
  };
}
