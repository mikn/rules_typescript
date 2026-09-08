### Fixed

- **A `.tsx` under `jsx: "preserve"` emits `.jsx`, the name tsc gives it.**
  The rule declared `foo.js` for every `.tsx` and oxc wrote the preserved JSX
  into it, so vitest refused the file (`Failed to parse source for import
  analysis ... make sure to name the file with the .jsx or .tsx extension`; on
  Vite 7, `RollupError: Parse failure: Expression expected`). The rule names
  its outputs before any action can read the tsconfig, so `ts_config` gains
  `jsx = "preserve"`, the one compiler option that names an output; `ts_compile`
  and the `ts_compile` a `ts_test` generates declare `foo.jsx` and `foo.jsx.map`
  from it, oxc-bazel names the file `.jsx` under `--jsx preserve`, and the
  `TsConfig` action fails a target with a `.tsx` src when the declaration and
  the chain's effective `jsx` disagree, naming the edit. A `ts_test` runs a
  `.jsx` test file and resolves a `.tsx` setup file to its `.jsx`. The hub's
  view of a workspace member rewrites a `.tsx` target in the member's manifest
  to the `.jsx` when the compiling target's `ts_config` declares `preserve`:
  the rewrite moves from the module extension to the view, at analysis, where
  the declaration is readable. Gazelle writes the attribute from the
  `extends` chain, leaf-wins, and removes it otherwise. A tsconfig passed as a
  plain file declares nothing; one that sets `preserve` fails the `TsConfig`
  action of a target with a `.tsx` src without a `ts_config`.
