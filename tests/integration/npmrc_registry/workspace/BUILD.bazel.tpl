"""Root BUILD file for the npmrc_registry integration test workspace."""

load("@gazelle//:def.bzl", "gazelle")

# gazelle:ts_package_boundary every-dir

gazelle(
    name = "gazelle",
    gazelle = "@rules_typescript//gazelle:gazelle_ts",
)
