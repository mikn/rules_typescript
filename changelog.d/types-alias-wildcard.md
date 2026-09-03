### Fixed

- **A `@types/x` package now answers `x/*` as well as `x`, and both configs
  spell the wildcard the same way.** Three divergences between the tsconfig
  `ts_compile` generates and the one `ts_refresh_tsconfig` installs, each a
  specifier resolving in one and not the other:

  - The build gave the bare key to the `@types/*` package and left the runtime
    package's wildcard standing, so `@babel/core` resolved to
    `@types/babel__core/index.d.ts` while `@babel/core/*` resolved into
    `@babel/core`'s own repository, whose whole tree holds no `.d.ts` file.
    The alias's wildcard now displaces it, yielding to the same thing the bare
    key yields to, a `path_aliases` prefix.
  - The editor's wildcard listed only the entry point's own directory, so
    `vite/*` was `vite/dist/node/*` and nothing answered `vite/dist/node/index`.
    It now lists the package root first and the entry directory second, from
    `ts_compile`'s own `subpath_roots` rule, which is what the build has always
    done for a package with no `exports` map. 113 wildcard keys in this repo's
    own config gain that first substitution, and no key is added or removed.
  - `compilerOptions.types = ["node"]` stayed in the nested tsconfig an editor
    reads. The entry names a package TypeScript resolves by walking
    `node_modules/@types`, and there is none here, so `tsc` reports
    `TS2688: Cannot find type definition file for 'node'` (measured against
    typescript 5.9.2) where `tsgo` reports nothing. The editor's copy of
    `ts_compile`'s resolver recognised `pkg` and `pkg/sub` and not the bare name
    a paired `@types/*` package supplies. There is now one resolver,
    `types_entry_file`, exported from `ts_compile` and called from both.

- **Two packages claiming one `paths` key no longer resolve by sort order.** A
  target whose closure holds `@types/x` and no `x` gives the alias the `x` key,
  and the aggregate config sees that beside another target's real `x`. npm's
  rule now picks, in the generated config and in the tsserver hook's fragment
  merge alike: `node_modules/x` first, `node_modules/@types/x` only when it
  holds no declarations. Before, the winner was whichever package name sorted
  first, and `@types/x` sorts before `x`.
