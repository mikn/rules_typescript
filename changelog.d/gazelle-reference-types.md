### Fixed

- **Gazelle follows a source's `/// <reference types="x" />` into `deps`.** A
  target's ambient `@types/*` deps came from the tsconfig's `types` list alone,
  so a `vite-env.d.ts` naming `google.maps` in a directive type-checked under
  `tsc`, which resolves the directive through `node_modules/@types`, and failed
  in the sandbox with `TS2503: Cannot find namespace 'google'` once the list did
  not name the package. A directive in the comments before a source's first
  token now maps as a `types` entry of that name does (`@types/<name>` for a
  bare name, the package itself for a scoped or subpath one), resolved through
  the lockfile as a bare specifier is, on every `ts_compile` and `ts_test`
  listing the source. A bare name whose `@types/<name>` the lockfile lacks takes
  the package called `<name>`, tsc's order; a name the lockfile does not answer
  gets no dep, and `# gazelle:ts_warn_unresolved true` lists it. The `types`
  attribute is unchanged.
