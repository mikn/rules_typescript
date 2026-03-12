import { defineConfig } from 'tsup';

export default defineConfig({
  entry: ['src/index.ts'],
  format: ['esm', 'cjs'],
  dts: true,
  sourcemap: true,
  clean: true,
  splitting: false,
  treeshake: true,
  target: 'node18',
  // vite is a peer dep — never bundle it.
  // chokidar ships with vite 6.x; mark external so the peer supplies it.
  external: ['vite', 'chokidar'],
});
