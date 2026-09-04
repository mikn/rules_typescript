### Added

- **`# gazelle:ts_js_srcs .mjs .cjs` admits JavaScript sources into generated
  `srcs`.** `ts_compile` has accepted `.js`/`.mjs`/`.cjs` in `srcs` all along,
  but Gazelle classified only `.ts` and `.tsx`, so a checked-in `helper.mjs`
  beside the `helper.test.ts` importing `./helper.mjs` belonged to no generated
  target and the import failed the type check as `TS2307`. The directive is the
  opt-in: admitting them unconditionally would put `eslint.config.mjs` and
  `postcss.config.mjs` into the type program of every repo.

  ```python
  # gazelle:ts_js_srcs .mjs .cjs
  ```

  The value is the whole set: it applies to a directory and below, a
  subdirectory naming one extension admits that one alone, and naming none
  returns the subtree to `.ts`/`.tsx`. Plain `.js` is refused by name.
  `ts_compile` already declares `<stem>.js` as the output of a `.ts` src of the
  same stem, so `foo.js` beside `foo.ts` would be one file declared twice.
  Admission is about `srcs` and nothing else: an admitted `.mjs` does not make a
  directory a package under `# gazelle:ts_package_boundary tsconfig` (a
  `tsconfig.json` does).
