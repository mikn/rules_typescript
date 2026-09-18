### Fixed

- **A consumer on a rules_rust other than the ruleset's builds `oxc-bazel`
  again.** The crates it is built from were a `crate_universe` hub pinned by
  `oxc_cli/Cargo.Bazel.lock`, whose digest covers cargo-bazel's and the host
  tools' versions, so a consumer on rules_rust 0.70.0 failed every analysis
  that reaches `oxc-bazel` with ``Digests do not match`` and ``The current
  `lockfile` is out of date for 'rules_typescript_crates'``. The rendering
  is vendored instead: `oxc_cli/crates/` holds the BUILD files
  `tools/vendor_crates.sh` renders at the rules_rust floor, 0.70.0, and the
  ruleset's own extension declares the crate archives, so a consumer's
  rules_rust neither renders nor checks them. `oxc_cli/Cargo.Bazel.lock` is
  gone; `oxc_cli/Cargo.lock` stays the resolution.
