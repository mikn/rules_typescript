### Fixed

- **A `.ts` subpath into a workspace member resolves to the compiled file
  under `ts_test`.** A member's file that imports a sibling member by name and
  a `.ts` path (`subpath-member/src/value.ts`, the shape an application package
  with no `exports` map is imported by) compiles -- tsgo maps the `.ts` to the
  `.d.ts` under the view -- and failed at run time on both runners, `Cannot
  find package 'subpath-member/src/value.ts'` under vitest and
  `ERR_MODULE_NOT_FOUND` under node:test, because only a relative `.ts`
  specifier was mapped to its compiled sibling. The vitest layer's plugin and
  the node:test hook now map every `.ts`, `.tsx`, `.mts` or `.cts` specifier,
  relative or bare, to the compiled file that resolves from the importer, and
  leave one that resolves to nothing as written. A relative path that leaves
  the member resolves under pnpm's symlink to the member's directory alone
  and stays unresolved from its store tree here; the npm guide says why.
