### Changed

- **npm: the store, one tree per snapshot, cached, built by tsaction.** Every
  lockfile's package declares pnpm's virtual store through
  `npm_virtual_store()`, loaded from its hub's `defs.bzl`: one `npm_store`
  target per snapshot, named after its tree
  `node_modules/.pnpm/<name with / as +>@<version>[_<peer id>]/node_modules/<name>`,
  whose one action copies the fetched package's files into that tree with
  `tsaction stage` and declares one relative symlink beside it per dependency
  edge, the alias name for an npm alias; one `npm_store_member` per workspace
  member, `<name with / as +>@0.0.0`, holding the member's package.json as
  built, its `.js`, `.js.map`, `.d.ts` and data at their package-relative
  paths, with the links its importer declares; and one `npm_store_hoist`,
  pnpm's hidden hoist, a link per hoisted name under
  `node_modules/.pnpm/node_modules/` (and under the root importer's
  `node_modules/` for a `public-hoist-pattern` match) to the resolution pnpm's
  own hoist step picks, from the lockfile package's `.npmrc`. A tree holds no
  symlink, so it is hashed as files and restored as files from a disk or
  remote cache in every download mode; the links are internal actions. Every
  `ts_npm_package` carries its store as `NpmPackageInfo.store`, a member's hub
  view carries the member's, and the lockfile's repository is where a link's
  relative target has one text in the execroot and the runfiles tree, so the
  `npm` extension refuses two lockfiles in one package and a consumer that
  took `@npm` from the ruleset's own lockfile declares one of its own
  (`e2e/basic`). `NpmPackageInfo.js_files` and `transitive_package_dirs`,
  which nothing read, are gone; the hub view no longer writes the manifest or
  reads the member's direct deps. The forest `ts_compile` and `ts_test` stage
  is unchanged by this: nothing consumes the store yet.
