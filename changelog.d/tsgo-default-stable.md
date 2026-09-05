### Changed

- **The default compiler is the `typescript` 7.0.2 release, read from
  `ts/private/tsgo/pnpm-lock.yaml`, not the `@typescript/native-preview`
  nightly `7.0.0-dev.20260311.1`.** A workspace that calls no `ts.tsgo()` moves
  with it. The file the toolchain points at is now named `lib/tsc`;
  `TsgoToolchainInfo.tsgo_binary` and every `ts_compile` action are unchanged.
  `ts.tsgo(version = ...)` still downloads a named release, now always
  unverified: the sha256 table that covered the one nightly is gone, and the
  lockfile is the verified path.
