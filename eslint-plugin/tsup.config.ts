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
  // ESLint and @typescript-eslint/utils are peer deps — never bundle them.
  external: ['eslint', '@typescript-eslint/utils', '@typescript-eslint/types'],
  // Use named exports for CJS to avoid `chunk.default` consumer confusion.
  cjsInterop: true,
});
