"""Analysis-time proof of what the IDE tsconfig says: ambient types, module paths,
npm pairing, and the per-package programs the root block cannot carry.

An @types/* package reaches the compiler through the entry point a consumer names
in `files`, never through a module specifier, so no `paths` entry can stand in
for it -- and the siblings that entry point references have to be installed
beside it.
"""

load("@bazel_skylib//lib:unittest.bzl", "analysistest", "asserts")
load("//ts/private:tsconfig_aspect.bzl", "WorkspaceCopyInfo")

def _written_config(env):
    for action in analysistest.target_actions(env):
        outputs = action.outputs.to_list()
        if len(outputs) == 1 and outputs[0].basename.endswith(".json"):
            return json.decode(action.content)
    return None

def _nested_config(env, dest):
    """The nested tsconfig written for `dest`, found through the copy it declares.

    Not by basename: the root config and every nested one are single-output .json
    writes, and _written_config would return whichever came first.
    """
    for entry in analysistest.target_under_test(env)[WorkspaceCopyInfo].entries.to_list():
        if entry.dest != dest:
            continue
        for action in analysistest.target_actions(env):
            if entry.file in action.outputs.to_list():
                return json.decode(action.content)
    return None

def _installed(env):
    return [
        entry.dest
        for entry in analysistest.target_under_test(env)[WorkspaceCopyInfo].entries.to_list()
    ]

def _ambient_types_impl(ctx):
    env = analysistest.begin(ctx)
    config = _written_config(env)
    asserts.true(env, config != None, "ide_tsconfig wrote no tsconfig")
    if config == None:
        return analysistest.end(env)

    asserts.equals(
        env,
        ["./.bazel/npm/@types/node/index.d.ts"],
        config.get("files"),
        "@types/node's entry point is named in `files`",
    )

    # A `files` array of its own switches off TypeScript's implicit `include`,
    # and with it every source in the workspace.
    asserts.equals(
        env,
        ["**/*"],
        config.get("include"),
        "the implicit include is spelled out alongside it",
    )

    installed = _installed(env)
    asserts.true(
        env,
        ".bazel/npm/@types/node/index.d.ts" in installed,
        "the entry point is installed under npm_dir: " + str(installed),
    )

    # index.d.ts is little more than a list of `/// <reference path=...>`, each
    # resolved on disk beside it.
    asserts.true(
        env,
        ".bazel/npm/@types/node/globals.d.ts" in installed,
        "the siblings it references are installed too: " + str(installed),
    )
    asserts.true(
        env,
        ".bazel/npm/@types/node/package.json" in installed,
        "the package.json is installed too: " + str(installed),
    )

    # A typeRoot is a directory whose *children* are the type packages, and one
    # npm repo per package leaves no such directory to name.
    asserts.equals(
        env,
        None,
        config["compilerOptions"].get("typeRoots"),
        "no typeRoots is derived",
    )
    return analysistest.end(env)

ambient_types_test = analysistest.make(_ambient_types_impl)

def _no_ambient_types_impl(ctx):
    env = analysistest.begin(ctx)
    config = _written_config(env)
    asserts.true(env, config != None, "ide_tsconfig wrote no tsconfig")
    if config == None:
        return analysistest.end(env)

    # No @types/* dep, so no `files` -- and therefore no reason to spell the
    # implicit include out. Both keys absent is what the test above is measured
    # against.
    asserts.equals(env, None, config.get("files"), "nothing is named in `files`")
    asserts.equals(env, None, config.get("include"), "the implicit include is left implicit")
    asserts.equals(
        env,
        [],
        [d for d in _installed(env) if "@types" in d],
        "nothing from an @types package is installed",
    )
    return analysistest.end(env)

no_ambient_types_test = analysistest.make(_no_ambient_types_impl)

def _module_paths_impl(ctx):
    env = analysistest.begin(ctx)
    config = _written_config(env)
    asserts.true(env, config != None, "ide_tsconfig wrote no tsconfig")
    if config == None:
        return analysistest.end(env)

    paths = config["compilerOptions"]["paths"]

    # The bare specifier an import writes. Nothing else in the file carries it:
    # the target's package path is what the label says, not what the import does.
    asserts.equals(
        env,
        ["./tests/lsp/module_fixture/index"],
        paths.get("@acme/widget"),
        "module_name resolves to the declaring package's entry point",
    )
    asserts.equals(
        env,
        ["./tests/lsp/module_fixture/*", "./bazel-bin/tests/lsp/module_fixture/*"],
        paths.get("@acme/widget/*"),
        "and its subpaths reach both the sources and the generated declarations",
    )

    # A module_name is an addition, not a replacement: the package path still
    # resolves, since a relative import from a sibling package uses it.
    asserts.equals(
        env,
        ["./tests/lsp/module_fixture/*", "./bazel-bin/tests/lsp/module_fixture/*"],
        paths.get("tests/lsp/module_fixture/*"),
        "the package-path key survives alongside it",
    )

    # Every value, not just the module's: a bare one is a module specifier to
    # TypeScript, which is TS5090 in the compile tsconfig and a silently
    # unresolved import here.
    asserts.equals(
        env,
        [],
        [v for key in sorted(paths) for v in paths[key] if not v.startswith(".")],
        "every paths value is visibly relative",
    )

    asserts.true(
        env,
        "**/vendor" in config["exclude"],
        "extra_exclude reaches the generated exclude: " + str(config["exclude"]),
    )
    asserts.true(
        env,
        "**/node_modules" in config["exclude"],
        "and the built-in globs are still there: " + str(config["exclude"]),
    )
    return analysistest.end(env)

module_paths_test = analysistest.make(_module_paths_impl)

def _transitive_types_pairing_impl(ctx):
    env = analysistest.begin(ctx)
    config = _written_config(env)
    asserts.true(env, config != None, "ide_tsconfig wrote no tsconfig")
    if config == None:
        return analysistest.end(env)

    # chai ships no declarations of its own and is reached only transitively
    # (vitest -> @vitest/expect -> chai). Read from the direct deps alone the
    # pairing is invisible: the entry still names a directory, but nothing
    # installs @types/chai's declarations into it, so the editor resolves chai to
    # a lone package.json where the build resolves it to the types.
    paths = config["compilerOptions"]["paths"]
    asserts.equals(
        env,
        ["./.bazel/npm/chai"],
        paths.get("chai"),
        "an untyped transitive package resolves to its @types/* directory",
    )
    asserts.equals(
        env,
        None,
        paths.get("@types/chai"),
        "and the @types/* package itself gets no entry of its own",
    )

    installed = _installed(env)
    asserts.true(
        env,
        ".bazel/npm/chai/index.d.ts" in installed,
        "@types/chai's declarations are what is installed there: " +
        str([d for d in installed if "chai" in d]),
    )
    return analysistest.end(env)

transitive_types_pairing_test = analysistest.make(_transitive_types_pairing_impl)

def _npm_json_installed_impl(ctx):
    env = analysistest.begin(ctx)

    # ts_compile stages the same set into its sandbox (ts_compile.bzl's
    # npm_json_depset); this is the editor half of that.
    installed = _installed(env)
    asserts.true(
        env,
        ".bazel/npm/entities/src/generated/.eslintrc.json" in installed,
        "a package's non-manifest .json is installed beside its declarations: " +
        str([d for d in installed if "entities/" in d and d.endswith(".json")]),
    )

    # A nested package.json is not inert data: it is the nearest manifest whose
    # `type` a staged .d.ts inherits, and it decides what a directory-shaped
    # `<pkg>/*` match resolves to. 22 of the 54 files this adds over the repo's
    # own closure are these, so they are the half worth pinning.
    asserts.true(
        env,
        ".bazel/npm/entities/dist/commonjs/package.json" in installed,
        "a nested manifest comes with it: " +
        str([d for d in installed if "entities/" in d and d.endswith("package.json")]),
    )
    return analysistest.end(env)

npm_json_installed_test = analysistest.make(_npm_json_installed_impl)

_MERGED_PACKAGE = "tests/lsp/option_groups"

def _option_merge_impl(ctx):
    env = analysistest.begin(ctx)
    config = _nested_config(env, _MERGED_PACKAGE + "/tsconfig.json")
    asserts.true(env, config != None, "no nested tsconfig was written for " + _MERGED_PACKAGE)
    if config == None:
        return analysistest.end(env)

    # One directory, one program: two targets that disagree about nothing get a
    # single block holding what each of them asked for.
    options = config["compilerOptions"]
    asserts.equals(env, True, options.get("noUnusedParameters"), "one target's key survives")
    asserts.equals(env, True, options.get("noFallthroughCasesInSwitch"), "and so does the other's")
    asserts.equals(
        env,
        ["fallthrough.ts", "params.ts"],
        config["include"],
        "both targets' sources are in that program",
    )
    return analysistest.end(env)

option_merge_test = analysistest.make(_option_merge_impl)

def _member_paths_impl(ctx):
    env = analysistest.begin(ctx)
    config = _written_config(env)
    asserts.true(env, config != None, "ide_tsconfig wrote no tsconfig")
    if config == None:
        return analysistest.end(env)

    paths = config["compilerOptions"]["paths"]

    # What the member's package.json designates, in the source tree and under
    # bazel-bin, with the guesses kept behind it -- the same map the tsconfig
    # ts_compile generates carries, so an editor and the build resolve
    # `pulse/button` to the same file.
    asserts.equals(
        env,
        [
            "./packages/pulse/entry",
            "./bazel-bin/packages/pulse/entry",
            "./packages/pulse/dist/entry",
            "./bazel-bin/packages/pulse/dist/entry",
            "./packages/pulse/index",
        ],
        paths.get("pulse"),
        "pulse: " + str(paths.get("pulse")),
    )
    asserts.equals(
        env,
        [
            "./packages/pulse/components/controls/button/index",
            "./bazel-bin/packages/pulse/components/controls/button/index",
            "./packages/pulse/button",
            "./bazel-bin/packages/pulse/button",
        ],
        paths.get("pulse/button"),
        "pulse/button: " + str(paths.get("pulse/button")),
    )
    asserts.equals(
        env,
        [
            "./packages/pulse/styles/tokens/*",
            "./bazel-bin/packages/pulse/styles/tokens/*",
            "./packages/pulse/tokens/*",
            "./bazel-bin/packages/pulse/tokens/*",
        ],
        paths.get("pulse/tokens/*"),
        "pulse/tokens/*: " + str(paths.get("pulse/tokens/*")),
    )

    # Everything the manifest does not name still goes through the wildcard the
    # map has always had.
    asserts.equals(
        env,
        ["./packages/pulse/*", "./bazel-bin/packages/pulse/*"],
        paths.get("pulse/*"),
        "pulse/*: " + str(paths.get("pulse/*")),
    )
    asserts.equals(
        env,
        [],
        [key for key in paths if key.startswith("pulse/internal") or key.endswith(".css")],
        "a subpath designating no declaration got a key: " + str(sorted(paths)),
    )
    return analysistest.end(env)

member_paths_test = analysistest.make(_member_paths_impl)

def _fails_with(message):
    def _impl(ctx):
        env = analysistest.begin(ctx)
        asserts.expect_failure(env, message)
        return analysistest.end(env)

    return analysistest.make(_impl, expect_failure = True)

option_conflict_test = _fails_with("compilerOptions.noUnusedLocals to ")
root_value_conflict_test = _fails_with("compilerOptions.strict to ")
baseline_conflict_test = _fails_with("extend the tsconfig baselines ")
