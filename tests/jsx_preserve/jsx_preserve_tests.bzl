"""What the rule declares for a .tsx under jsx: preserve, before any action,
and what the hub's view of a member under it writes."""

load("@bazel_skylib//lib:unittest.bzl", "analysistest", "asserts")
load("//ts/private:providers.bzl", "NpmPackageInfo")

_PKG = "tests/jsx_preserve/"

def _declared_outputs_impl(ctx):
    env = analysistest.begin(ctx)
    target = analysistest.target_under_test(env)
    got = sorted([
        f.path[f.path.find(_PKG) + len(_PKG):]
        for f in target[DefaultInfo].files.to_list()
    ])
    asserts.equals(
        env,
        ["view.d.ts", "view.jsx", "view.jsx.map"],
        got,
        "declared outputs",
    )

    config = [
        a
        for a in analysistest.target_actions(env)
        if a.mnemonic == "TsConfig"
    ]
    asserts.equals(env, 1, len(config), "TsConfig actions")
    if len(config) == 1:
        asserts.true(
            env,
            "-jsx=preserve" in config[0].argv,
            "the TsConfig step is told the declaration",
        )
    return analysistest.end(env)

declared_outputs_test = analysistest.make(_declared_outputs_impl)

def _member_view_impl(ctx):
    env = analysistest.begin(ctx)
    written = [
        a
        for a in analysistest.target_actions(env)
        if len(a.outputs.to_list()) == 1 and
           a.outputs.to_list()[0].basename == "package.json"
    ]
    asserts.equals(env, 1, len(written), "the view writes one package.json")
    if len(written) == 1:
        manifest = json.decode(written[0].content)
        asserts.equals(
            env,
            "./view.jsx",
            manifest.get("exports"),
            "the exports target names the emitted .jsx",
        )

    info = analysistest.target_under_test(env)[NpmPackageInfo]
    root = info.package_root + "/"
    linked = sorted([
        f.path[len(root):] if f.path.startswith(root) else f.basename
        for f in info.all_files.to_list()
    ])
    asserts.equals(
        env,
        [
            "jsx-runtime.d.ts",
            "jsx-runtime.js",
            "jsx-runtime.js.map",
            "package.json",
            "view.d.ts",
            "view.jsx",
            "view.jsx.map",
        ],
        linked,
        "the link holds the .jsx at the path the manifest names",
    )
    return analysistest.end(env)

member_view_test = analysistest.make(_member_view_impl)
