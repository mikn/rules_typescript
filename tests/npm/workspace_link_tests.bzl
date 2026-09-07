"""Which target the hub depends on for a pnpm `link:` workspace member.

A member is a directory, and which directory inside it holds the target that
compiles it is not the member's to say: `ts_package_boundary` decides it, and
`ts_target_name` renames the result. Both are Gazelle directives, read from BUILD
files that need not exist when the hub is generated -- so the hub enumerates the
directories that could hold the target and looks them up, innermost first,
rather than deriving one from the member's entry point.

The lookup itself is exercised end to end by
//tests/npm:workspace_link_target_test and
//tests/npm:workspace_link_target_files_test, over three members that disagree
about which candidate is the right one. What is pinned here is what a passing
build cannot show.

The candidate list: drop the entry point's own directory and two of those three
members have no target; drop the member's root and the third one has none.

The entry points the manifest is read for: a member that declares only an
`exports` map -- no `main`, no `module` -- has designated an entry point, and a
reader that stops at `main` does not see it.

A member with no target at all, `packages/no-target-member`: the hub writes a
comment and NO LABEL. A BUILD file that declares nothing named after the member
is the statement that the target is not there, and a label written anyway would
name a target Bazel cannot resolve -- which fails analysis for every consumer of
the hub, while a missing target fails only what asks for the member. Nothing
built from the hub can tell those two apart, because both fail whatever names the
member, so the generated text is the only place the difference is visible. A
member whose directory holds no package.json with a name gets the same
treatment: the view carries that manifest, so there is nothing to write.

What the view carries, pinned on @npm//:shared: the member's package.json with
every source-file target under `exports` rewritten to the emitted file, the
member's .js and .d.ts at the paths that manifest names, and its data srcs at
their package-relative paths, where the .js reaches them at run time. tsc maps a
`.js` target to the `.d.ts` beside it and node runs the `.js`, so one manifest
serves //tests/npm:workspace_consumer's check and :workspace_runtime_test's run.
"""

load("@bazel_skylib//lib:unittest.bzl", "analysistest", "asserts", "unittest")
load(
    "//npm/private:npm_import.bzl",
    "link_block",
    "link_candidate_dirs",
    "link_target_label",
    "manifest_entries",
    "target_name_in",
)
load("//ts/private:providers.bzl", "NpmPackageInfo")

_CASES = [
    struct(
        shape = "an entry under src/, the ordinary layout",
        path = "packages/workers-wide-event-sink",
        entries = ["src/index.ts"],
        expected = [
            "packages/workers-wide-event-sink/src",
            "packages/workers-wide-event-sink",
        ],
    ),
    struct(
        shape = "the same, written with a leading ./",
        path = "packages/foo",
        entries = ["./src/index.ts"],
        expected = ["packages/foo/src", "packages/foo"],
    ),
    struct(
        shape = "an entry at the member root",
        path = "packages/bar",
        entries = ["index.ts"],
        expected = ["packages/bar"],
    ),
    struct(
        shape = "a member that designates no entry at all",
        path = "packages/baz",
        entries = [],
        expected = ["packages/baz"],
    ),
    struct(
        shape = "a nested entry directory, innermost first",
        path = "web",
        entries = ["dist/lib/index.js"],
        expected = ["web/dist/lib", "web/dist", "web"],
    ),
    struct(
        shape = "two entry points naming one directory: it is a candidate once",
        path = "packages/qux",
        entries = ["./src/index.ts", "./src/index.js"],
        expected = ["packages/qux/src", "packages/qux"],
    ),
    struct(
        shape = "depth orders the candidates across entry points and not one " +
                "entry point at a time: the root is reached through both, and " +
                "still comes last",
        path = "packages/quux",
        entries = ["./dist/index.d.ts", "./src/index.ts"],
        expected = ["packages/quux/dist", "packages/quux/src", "packages/quux"],
    ),
]

def _link_candidate_dirs_test(ctx):
    env = unittest.begin(ctx)
    for case in _CASES:
        asserts.equals(
            env,
            case.expected,
            link_candidate_dirs(case.path, case.entries),
            case.shape,
        )
    return unittest.end(env)

_ENTRY_CASES = [
    struct(
        shape = "main and module, in that order",
        manifest = {"main": "dist/index.js", "module": "./src/index.ts"},
        expected = ["dist/index.js", "./src/index.ts"],
    ),
    struct(
        shape = "module alone, for a member that publishes no CJS entry",
        manifest = {"module": "./src/index.ts"},
        expected = ["./src/index.ts"],
    ),
    struct(
        shape = "exports as a bare string",
        manifest = {"exports": "./src/index.ts"},
        expected = ["./src/index.ts"],
    ),
    struct(
        shape = "exports keyed by '.', the shape @lovable/canvas-sdk had",
        manifest = {"exports": {".": "./src/index.ts"}},
        expected = ["./src/index.ts"],
    ),
    struct(
        shape = "a condition map under '.', in the order the map is written",
        manifest = {
            "exports": {".": {"types": "./src/index.ts", "import": "./dist/index.js"}},
        },
        expected = ["./src/index.ts", "./dist/index.js"],
    ),
    struct(
        shape = "conditions with no subpath key at all, npm's shorthand for a " +
                "package that exports nothing but itself",
        manifest = {"exports": {"default": "./src/index.ts"}},
        expected = ["./src/index.ts"],
    ),
    struct(
        shape = "a fallback array",
        manifest = {"exports": {".": ["./src/index.ts", "./dist/index.js"]}},
        expected = ["./src/index.ts", "./dist/index.js"],
    ),
    struct(
        shape = "main first, then what exports adds: both directories are " +
                "candidates, and neither field overrules the other",
        manifest = {"main": "./dist/index.js", "exports": {".": "./src/index.ts"}},
        expected = ["./dist/index.js", "./src/index.ts"],
    ),
    struct(
        shape = "the same path in main and exports, which is the common case: " +
                "one entry, not two",
        manifest = {"main": "./src/index.ts", "exports": {".": "./src/index.ts"}},
        expected = ["./src/index.ts"],
    ),
    struct(
        shape = "subpaths only: nothing designates the package's own entry point",
        manifest = {"exports": {"./sub": "./src/sub.ts"}},
        expected = [],
    ),
    struct(
        shape = "a wildcard target designates a pattern, whose directory is one too",
        manifest = {"exports": {".": "./src/*.ts"}},
        expected = [],
    ),
    struct(
        shape = "a manifest that designates nothing",
        manifest = {"name": "member", "version": "0.0.0"},
        expected = [],
    ),
]

def _manifest_entries_test(ctx):
    env = unittest.begin(ctx)
    for case in _ENTRY_CASES:
        asserts.equals(env, case.expected, manifest_entries(case.manifest), case.shape)
    return unittest.end(env)

def _reader(build_files):
    """`link_target_label`'s BUILD-text lookup, over a fixed set of packages."""

    def build_text(dir_path):
        return build_files.get(dir_path)

    return build_text

_LOOKUP_CASES = [
    struct(
        shape = "the member root declares the target",
        member = "packages/shared",
        entries = [],
        build_files = {"packages/shared": 'ts_compile(name = "shared")'},
        expected = "@@//packages/shared:shared",
    ),
    struct(
        shape = "ts_target_name renames it, and the directive is what Gazelle used",
        member = "packages/boundary-member",
        entries = ["src/index.ts"],
        build_files = {
            "packages/boundary-member": '# gazelle:ts_target_name lib\nts_compile(name = "lib")',
        },
        expected = "@@//packages/boundary-member:lib",
    ),
    struct(
        shape = "the entry point's own directory wins over the root when both " +
                "are packages",
        member = "packages/leaf-member",
        entries = ["src/index.ts"],
        build_files = {
            "packages/leaf-member": 'ts_compile(name = "leaf-member")',
            "packages/leaf-member/src": 'ts_compile(name = "src")',
        },
        expected = "@@//packages/leaf-member/src:src",
    ),
    struct(
        shape = "an entry point read out of `exports` reaches the same directory " +
                "`main` would have",
        member = "packages/exports-member",
        entries = ["./src/index.ts"],
        build_files = {"packages/exports-member/src": 'ts_compile(name = "src")'},
        expected = "@@//packages/exports-member/src:src",
    ),
    struct(
        shape = "no BUILD file anywhere under the member",
        member = "packages/unbuilt",
        entries = ["src/index.ts"],
        build_files = {},
        expected = None,
    ),
    struct(
        shape = "a BUILD file that declares no target of the member's name -- " +
                "packages/no-target-member, whose root holds a lone ts_config. " +
                "A label here would name a target Bazel cannot resolve",
        member = "packages/no-target-member",
        entries = ["./src/index.ts"],
        build_files = {
            "packages/no-target-member": 'ts_config(name = "tsconfig", src = "tsconfig.json")',
        },
        expected = None,
    ),
    struct(
        shape = "the same, one directory down: a package beside the sources that " +
                "declares something else",
        member = "packages/other-target-member",
        entries = ["./src/index.ts"],
        build_files = {
            "packages/other-target-member/src": 'filegroup(name = "assets")',
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
            link_target_label(case.member, case.entries, _reader(case.build_files)),
            case.shape,
        )
    return unittest.end(env)

_MEMBERS = {"no-target-member": "no-target-member|packages/no-target-member"}
_MANIFEST = '{"name":"no-target-member","type":"module"}'

def _unresolved_link_block_test(ctx):
    env = unittest.begin(ctx)
    lines = link_block(_MEMBERS, {"packages/no-target-member": None}, {"no-target-member": _MANIFEST})

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

    unmanifested = link_block(_MEMBERS, {"packages/no-target-member": "@@//packages/x:x"}, {})
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
        "and gets no target: the view carries the manifest, so there is nothing to write",
    )

    resolved = link_block(_MEMBERS, {"packages/no-target-member": "@@//packages/x:x"}, {"no-target-member": _MANIFEST})
    asserts.equals(
        env,
        [
            "npm_workspace_package(",
            '    name = "no-target-member",',
            '    package_name = "no-target-member",',
            '    member_dir = "packages/no-target-member",',
            '    target = "@@//packages/x:x",',
            "    manifest_json = " + json.encode(_MANIFEST) + ",",
            ")",
            "",
        ],
        resolved,
        "the same link with a target and a manifest, so the cases above are a " +
        "missing label and not a missing block",
    )
    return unittest.end(env)

def _view_manifest(env):
    for action in analysistest.target_actions(env):
        outputs = action.outputs.to_list()
        if len(outputs) == 1 and outputs[0].basename == "package.json":
            return json.decode(action.content)
    return None

def _member_view_impl(ctx):
    env = analysistest.begin(ctx)
    manifest = _view_manifest(env)
    asserts.true(env, manifest != None, "the view writes no package.json")
    if manifest != None:
        asserts.equals(env, "shared", manifest.get("name"), "the member's own name")
        asserts.equals(env, "module", manifest.get("type"), "the emitted .js is ESM")
        asserts.equals(
            env,
            {".": "./src/index.js", "./wire": "./src/wire/index.js"},
            manifest.get("exports"),
            "every source-file target in exports names the emitted file: " + str(manifest.get("exports")),
        )

    info = analysistest.target_under_test(env)[NpmPackageInfo]
    asserts.equals(env, "shared", info.package_name, "the name the lockfile links the member by")
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
        ],
        sorted([
            f.path[len(root):] if f.path.startswith(root) else f.basename
            for f in info.all_files.to_list()
        ]),
        "the link holds the manifest, the member's .js and .d.ts at the " +
        "paths the manifest names, and its data srcs at their " +
        "package-relative paths",
    )
    return analysistest.end(env)

member_view_test = analysistest.make(_member_view_impl)

def _target_name_test(ctx):
    env = unittest.begin(ctx)
    asserts.equals(
        env,
        "events",
        target_name_in('ts_compile(name = "events")', "packages/events"),
        "the directory basename, which is what Gazelle names a target after",
    )
    asserts.equals(
        env,
        "lib",
        target_name_in(
            '# gazelle:ts_target_name lib\nts_compile(name = "lib")',
            "packages/events",
        ),
        "the directive wins: it is what Gazelle named the target after",
    )
    return unittest.end(env)

link_candidate_dirs_test = unittest.make(_link_candidate_dirs_test)
manifest_entries_test = unittest.make(_manifest_entries_test)
link_target_label_test = unittest.make(_link_target_label_test)
unresolved_link_block_test = unittest.make(_unresolved_link_block_test)
target_name_test = unittest.make(_target_name_test)

def workspace_link_test_suite(name):
    unittest.suite(
        name,
        link_candidate_dirs_test,
        manifest_entries_test,
        link_target_label_test,
        unresolved_link_block_test,
        target_name_test,
    )
