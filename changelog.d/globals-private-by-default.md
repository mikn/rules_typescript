### Breaking — ts_compile

- **A target's ambient globals are private to its own compilation unless
  `public_globals` names the file.** A `.d.ts` in `srcs` with no top-level
  import or export declares globals, and until now every one of them was
  injected into the program of every target that depends on it, transitively,
  with nothing to report it. A library's `declare const process` shim, real to
  its own standalone `tsc -p`, is `files[0]` of every consumer's generated
  tsconfig and `@types/node` is further down, so the shim's type wins every use
  site while the duplicate-identifier diagnostic stays inside a `.d.ts` where
  `skipLibCheck: true` silences it. What the consumer sees is errors about a
  package it never named.

  Migration: on the target that owns an ambient consumers are meant to have,
  name the file.

  ```python
  ts_compile(
      name = "worker_types",
      srcs = ["worker-configuration.d.ts"],
      public_globals = ["worker-configuration.d.ts"],   # add this
  )
  ```

  A consumer left without a global it needs fails on the identifier
  (`TS2304: Cannot find name '...'`), which names the file to export. Every
  entry must be in `srcs` and must be global; naming a module-scoped `.d.ts`
  fails the build.

  `vite_types = True` is this rule applied to the shim it prepends. The shim is
  a src of the target that sets the attribute and of no other, so a consumer
  using `import.meta.env`, `import.meta.hot` or an asset-URL import has to set
  `vite_types = True` itself. `ImportMeta` is in `lib`, so that consumer fails
  on the member, not the name:
  `TS2339: Property 'env' does not exist on type 'ImportMeta'`.
