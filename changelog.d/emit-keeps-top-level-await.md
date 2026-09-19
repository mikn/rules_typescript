### Fixed

- **A top-level `await` survives the emit under a `target` below `es2022`.**
  tsc keeps one wherever `module` admits it -- `es2022`, `esnext`, `system`,
  `preserve`, the `node*` kinds -- under a `target` of `es2017` or later, and
  never lowers it; `TsEmit` handed oxc the chain's `target` alone, and oxc's
  handling of a top-level `await` below `es2022` is a refusal: `Transform
  error(s): Top-level await is not available in the configured target
  environment`. tsaction now passes oxc-bazel `--top-level-await` under tsc's
  rule (TS1378's), so a program tsc accepts emits, and the rest of the
  lowering follows the `target` as before.
  `//tests/compiler_options/top_level_await` pins it under `target: es2020`,
  `module: esnext`.
