### Fixed

- **A program a vitest test runs is ES modules whatever its tsconfig's
  `module`.** Since the emit follows `module`, a `module: "commonjs"` package's
  tests ran tsgo's CommonJS, and vitest refuses a CommonJS importer (`Vitest
  cannot be imported in a CommonJS module using require()` at the setup file's
  first line). vitest imports every file through vite's transform and the
  checkout never runs tsc's output under it, so `module` describes the
  package's published output, not its tests. A `ts_test` under the vitest
  runner now emits its own srcs as ES modules (`TsEmit -es_modules`, oxc's
  transform reading the srcs and the options alone), and a `ts_compile` whose
  program tsgo emits also emits the ES twin of each `.js` under `<name>.es/`,
  which the vitest runner stages at the `.js`'s runfiles path; node:test and
  every other consumer run the `.js`. The rule names the twins before any
  action reads the tsconfig, so `ts_config` gains `module`, the chain's
  effective value when tsgo emits it (`"commonjs"`, `"node16"`, `"node18"`,
  `"nodenext"`), written by Gazelle from the `extends` chain and checked by the
  `TsConfig` action against the file, as `jsx = "preserve"` is; a plain-file
  tsconfig of such a program fails that check without a `ts_config`. The twins
  travel in `TsInfo.transitive_es_twins`, and a runner asks for them with
  `TsTestRunnerInfo.es_modules`. `//tests/vitest/commonjs` pins the run.
