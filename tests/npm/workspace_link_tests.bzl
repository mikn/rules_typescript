"""The hub's view of a `link:` member: the one target it names, what it writes
when the member's BUILD file declares none, and what the view carries. The
manifest the member's store writes is tests/npm/store_tests.bzl's."""

load("@bazel_skylib//lib:unittest.bzl", "analysistest", "asserts", "unittest")
load("//npm/private:npm_import.bzl", "link_block", "link_target_label")
load("//ts/private:providers.bzl", "NpmPackageInfo")

def _reader(build_files):
    """`link_target_label`'s BUILD-text lookup, over a fixed set of packages."""

    def build_text(dir_path):
        return build_files.get(dir_path)

    return build_text

# A member's compiling target is //<member>:<basename>; a label only when the
# BUILD file there declares it, since one naming nothing fails every consumer.
_LOOKUP_CASES = [
    struct(
        shape = "the member root declares the target",
        member = "packages/shared",
        build_files = {"packages/shared": 'ts_compile(name = "shared")'},
        expected = "@@//packages/shared:shared",
    ),
    struct(
        shape = "a BUILD file under src/ alone: the package model never puts " +
                "a member's compiling target there",
        member = "packages/leaf-member",
        build_files = {"packages/leaf-member/src": 'ts_compile(name = "src")'},
        expected = None,
    ),
    struct(
        shape = "no BUILD file anywhere under the member",
        member = "packages/unbuilt",
        build_files = {},
        expected = None,
    ),
    struct(
        shape = "a BUILD file that declares no target of the member's name -- " +
                "packages/no-target-member, whose root holds a lone ts_config",
        member = "packages/no-target-member",
        build_files = {
            "packages/no-target-member": 'ts_config(name = "tsconfig", src = "tsconfig.json")',
        },
        expected = None,
    ),
    struct(
        shape = "a target of another name at the root is not the member's, " +
                "whatever a stale ts_target_name line beside it says",
        member = "packages/boundary-member",
        build_files = {
            "packages/boundary-member": "# gazelle:ts_target_name lib\n" +
                                        'ts_compile(name = "lib")',
        },
        expected = None,
    ),
]

def _link_target_label_test(ctx):
    env = unittest.begin(ctx)
    for case in _LOOKUP_CASES:
        asserts.equals(
            env,
            case.expected,
            link_target_label(case.member, _reader(case.build_files), ""),
            case.shape,
        )
    return unittest.end(env)

_MEMBERS = {"no-target-member": "no-target-member|packages/no-target-member"}
_MANIFEST = '{"name":"no-target-member","type":"module"}'
_LOCK = struct(repo = "", package = "tests/npm")

def _unresolved_link_block_test(ctx):
    env = unittest.begin(ctx)
    lines = link_block(
        _MEMBERS,
        {"packages/no-target-member": None},
        {"no-target-member": _MANIFEST},
        _LOCK,
    )

    asserts.equals(
        env,
        1,
        len([
            line
            for line in lines
            if line.startswith("# NO TARGET for 'no-target-member'.")
        ]),
        "the member is named in a comment: nothing else says why it is missing",
    )
    asserts.equals(
        env,
        [],
        [line for line in lines if line and not line.startswith("#")],
        "a member with no target emits comment lines and nothing else -- a " +
        "label, even one this hub would never load itself, fails analysis for " +
        "every consumer of the hub",
    )

    unmanifested = link_block(
        _MEMBERS,
        {"packages/no-target-member": "@@//packages/x:x"},
        {},
        _LOCK,
    )
    asserts.equals(
        env,
        1,
        len([line for line in unmanifested if line.startswith("# NO MANIFEST for 'no-target-member'.")]),
        "a member whose directory holds no package.json with a name is named in a comment",
    )
    asserts.equals(
        env,
        [],
        [line for line in unmanifested if line and not line.startswith("#")],
        "and gets no target: the store writes the manifest, so there is " +
        "nothing to write",
    )

    resolved = link_block(
        _MEMBERS,
        {"packages/no-target-member": "@@//packages/x:x"},
        {"no-target-member": _MANIFEST},
        _LOCK,
    )
    asserts.equals(
        env,
        [
            "npm_workspace_package(",
            '    name = "no-target-member",',
            '    package_name = "no-target-member",',
            '    member_dir = "packages/no-target-member",',
            '    target = "@@//packages/x:x",',
            '    store = "@@//tests/npm:node_modules/.pnpm/' +
            'no-target-member@0.0.0/node_modules/no-target-member",',
            ")",
            "",
        ],
        resolved,
        "the same link with a target and a manifest, so the cases above are a " +
        "missing label and not a missing block",
    )
    return unittest.end(env)

def _member_view_impl(ctx):
    env = analysistest.begin(ctx)
    info = analysistest.target_under_test(env)[NpmPackageInfo]
    asserts.equals(env, "shared", info.package_name, "the name the lockfile links the member by")
    asserts.equals(
        env,
        "shared@0.0.0",
        info.store.key,
        "the view names the member's store tree",
    )
    asserts.equals(
        env,
        "package.json",
        info.store.manifest.basename,
        "the manifest as built is the store's",
    )
    asserts.true(
        env,
        info.package_root.endswith("/packages/shared"),
        "the view's root is the member's directory under bazel-bin, not the compiling " +
        "target's package: " + info.package_root,
    )
    root = info.package_root + "/"
    asserts.equals(
        env,
        [
            "package.json",
            "src/banner.json",
            "src/index.d.ts",
            "src/index.js",
            "src/index.js.map",
            "src/wire/index.d.ts",
            "src/wire/index.js",
            "src/wire/index.js.map",
            "src/wire/package.json",
        ],
        sorted([
            f.path[len(root):] if f.path.startswith(root) else f.basename
            for f in info.all_files.to_list()
        ]),
        "the view holds the manifest as built, the member's .js and .d.ts at " +
        "the paths the manifest names, and its data srcs at their " +
        "package-relative paths, the member's own package.json excepted",
    )
    asserts.equals(
        env,
        ["zod@3.24.2"],
        [d.package_name + "@" + d.package_version for d in info.direct_deps],
        "the view carries the compiling target's direct npm deps: the forest " +
        "places a member's own resolution of a name under the member",
    )
    return analysistest.end(env)

member_view_test = analysistest.make(_member_view_impl)

link_target_label_test = unittest.make(_link_target_label_test)
unresolved_link_block_test = unittest.make(_unresolved_link_block_test)

def workspace_link_test_suite(name):
    unittest.suite(
        name,
        link_target_label_test,
        unresolved_link_block_test,
    )
