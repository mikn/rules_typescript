### Changed

- **A workspace member's `package.json` as built stands wherever the runtime
  reads the member's manifest.** Under the package model the member's source
  `package.json` is a src of its `ts_compile`, so it is a data file too; the
  member's store tree leaves that one data src out, and `ts_test` stages the
  manifest as built at the member's own path in the runfiles. A test inside the
  member that imports the member by name resolves through the nearest
  `package.json` and reaches the emitted `.js`, not the source `.ts` targets.
