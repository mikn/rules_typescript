### Fixed

- **A scope `pnpm-workspace.yaml` maps to a registry is fetched from that
  registry, with pnpm's `auth` setting as its credential.** The extension read
  `registry=` and `@scope:registry=` from the `npmrc` label alone, so a
  workspace keeping its registries where pnpm 10+ keeps its settings, the
  `registries:` block of `pnpm-workspace.yaml`, had every such package's
  tarball URL composed on registry.npmjs.org: a 404 at the fetch. The
  extension now reads the `pnpm-workspace.yaml` beside the lockfile
  (`registries:`, then `registry:`) after the `.npmrc`'s lines, pnpm's order,
  and each fetch reads `PNPM_CONFIG__AUTH` -- pnpm's `auth` setting, JSON
  keyed by registry URL then scope, `authToken` its one field -- from its
  environment beside the `.npmrc`'s `//host/:_authToken=`, the package's scope
  entry first; the token enters no file and no lock. `npmrc_auth` and
  `npm/private/npmrc_auth.bzl` are `fetch_auth` and
  `npm/private/fetch_auth.bzl`, and the tsgo toolchain's repository takes the
  platform package's name for the same choice.
  `//tests/integration/npmrc_registry` is `//tests/integration/npm_registries`,
  one registry per file.
