"""Root BUILD file for the remix integration test workspace.

Everything else at this level -- node_modules, vite_bundler, ts_bundle -- is
what Gazelle generates from the @remix-run/* deps in package.json.
"""

load("@gazelle//:def.bzl", "gazelle")

gazelle(
    name = "gazelle",
    gazelle = "@rules_typescript//gazelle:gazelle_ts",
)

# Node resolves the exec-root vite_config symlink to the source file before its
# bare imports, so only a copy in bazel-out finds the hermetic node_modules tree.
genrule(
    name = "remix_vite_config",
    srcs = ["remix-vite.config.mjs"],
    outs = ["remix-vite.generated.config.mjs"],
    cmd = "cp $< $@",
)
