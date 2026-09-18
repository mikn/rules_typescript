### Changed

- **Gazelle lists the JavaScript a declaration stands for as a src.** tsc drops
  `x.mjs` from a program that holds `x.d.mts` and resolves `"./x.mjs"` to the
  declaration, so tsgo never lists the module itself; the `.mjs`, `.cjs` or
  `.js` beside an owned declaration of the same stem is a src of the same
  target, staged for the runtime and typed by the declaration.
