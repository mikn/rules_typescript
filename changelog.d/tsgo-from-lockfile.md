### Added

- **`ts.tsgo(pnpm_lock = "//:pnpm-lock.yaml")` makes the toolchain the
  TypeScript the lockfile pins.** TypeScript 7's `typescript` package is a
  launcher whose Go compiler lives in per-platform optional dependencies
  (`@typescript/typescript-linux-x64/lib/tsc` and the like), so a lockfile that
  pins `typescript` already states, for every platform, the tarball and the
  integrity of the compiler `tsc` runs. The `ts` extension reads the root
  importer's `typescript` entry for the version and those `packages:` entries
  for the bytes, and Bazel verifies each download against the lockfile's
  integrity; a `pnpm install` that moves the version moves the toolchain, with
  no version literal and no checksum table in `MODULE.bazel`.
  `package = "@typescript/native-preview"` reads the nightly's entry instead
  (its binary is `lib/tsgo`), and `npmrc` names the registry and its
  credentials as it does for `npm.translate_lock`. A lockfile with no
  `typescript`, one pinning a version
  before 7 (no platform packages), an alias under the name, or two versions
  with no root pin fails at extension evaluation naming the lockfile and the
  fix.
