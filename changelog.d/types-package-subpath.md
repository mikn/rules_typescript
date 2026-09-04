### Fixed

- **A `compilerOptions.types` entry naming a package subpath the manifest does
  not designate now resolves to the declaration the package ships there.**
  `types = ["@cloudflare/workers-types/2023-07-01"]` failed at analysis with
  `resolves to nothing`: the package has no `exports` map, so the rule had
  nothing to answer the subpath with, though `2023-07-01/index.d.ts` is in the
  package and is what TypeScript finds by walking `node_modules`. A subpath the
  manifest says nothing about is now looked for among the package's own
  declarations, in the order TypeScript reads them -- `<subpath>/index.d.ts`
  under the paired `@types/*` package, then the package's `<subpath>.d.ts` and
  `<subpath>/index.d.ts`, then `<subpath>.d.ts` under the `@types/*` package --
  and the file goes into the generated config's `files` and the editor's. The
  manifest's `typesVersions` mapping, a `package.json` inside the subpath's
  directory and the presence of an `exports` map that omits the subpath are not
  read, so a subpath `typesVersions` rewrites answers with the file at the
  spelled path where TypeScript reads the rewritten one
  (`web-streams-polyfill/dist/types/polyfill`). A subpath the manifest does
  designate resolves as before. An entry nothing answers still fails, the
  message naming every path it looked at, the paired `@types/*` package's
  included.
  A package the target names an entry of contributes that entry alone: a
  types-only package's root entry, which a direct dep on it otherwise puts in
  `files` unasked, stays out when the target named one of its subpaths, so the
  program holds one compatibility date's declarations rather than two.
