### Fixed

- **The action's root files keep the tsconfig's `include` order.** The written
  tsconfig listed every src in `include` in srcs order, so a program whose
  `include` named `svgr.d.ts` before `src/**/*` had `src/` first under Bazel,
  and where both declare `*.svg` the later pattern won: `tsc` passed and the
  action failed with `TS2345`. `tsaction tsconfig` now puts the roots
  `--showConfig` reports into `files`, in that order, and the other srcs into
  `include`.
