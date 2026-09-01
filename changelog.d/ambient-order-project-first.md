### Breaking — ts_compile

- **A project's own ambient declaration now beats one supplied by `types` or an
  `@types/*` dep, matching `tsc`.** The generated tsconfig listed
  `types`-supplied package ambients ahead of the project's own in `files`.
  Where two `declare module` blocks name the same pattern the first one in the
  program wins, so the package always won and a project had no way to override
  it. Native `tsc` resolves this the other way: a `types` package arrives as a
  type-reference directive, which joins the program *after* the root files, so
  the project's ambient wins. `files` now lists the project's ambients first.

  Concretely, a target with `compiler_options = {"types": ["vite/client"]}` and
  its own `declare module "*.svg"` typing the import as a component used to get
  vite's `string`; it now gets the component.

  If you were relying on the package ambient winning, drop the competing
  project ambient: order is the only lever, and a narrower pattern does not
  rescue you — an earlier `*.svg` still beats a later `*.icon.svg`.
