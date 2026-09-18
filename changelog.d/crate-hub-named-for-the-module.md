### Fixed

- A consumer can name its Cargo hub `crates`: the ruleset uses `rules_typescript_crates` for Oxc and `oj_crates` for oj. Both use rules_rs and resolve their own Cargo manifests and lockfiles.
