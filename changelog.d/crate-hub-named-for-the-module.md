### Fixed

- **A consumer whose own `crate_universe` hub is named `crates` builds
  again.** rules_rust evaluates its crate extension over every module's tags
  at once and fails when two modules name a hub alike -- ``Defined two crate
  universes with the same name in different MODULE.bazel files (`crates`)``
  -- and the ruleset's hub, the crates `oxc-bazel` is built from, was named
  `crates`, so such a consumer could analyse nothing that reaches
  `oxc-bazel`: no `ts_compile`, no `ts_test`. The hub is
  `rules_typescript_crates`; `crates` is the consumer's. MODULE.bazel declares
  it, a `use_repo_rule` repository over `oxc_cli/crates` (CONTRIBUTING.md
  § Vendoring the crates).
