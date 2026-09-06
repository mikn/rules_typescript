"""Analysis-time coverage for ts_compile.

Three kinds of assertion live here, none of which a build test can make:

  - what the rule writes at analysis and tells oxc -- the baseline tsconfig and
    the oxc command line, read straight out of the registered actions (the
    action tsconfig itself is tsaction's output; written_tsconfig_test.go reads
    it after the build);
  - that every guard fails, with the message that names the way out;
  - what a helper behind either one answers, called directly.

A guard's target is tagged manual so that `bazel build //...` does not try to
analyse it and stop on the very failure being asserted.
"""

load("@bazel_skylib//lib:unittest.bzl", "analysistest", "asserts", "unittest")
load("//ts:defs.bzl", "CssInfo")
load("//ts/private:ts_compile.bzl", "types_entry_declaration", "types_entry_file", "types_entry_package_ref")

def _written_file_action(env, suffix):
    for action in analysistest.target_actions(env):
        outputs = action.outputs.to_list()
        if len(outputs) == 1 and outputs[0].basename.endswith(suffix):
            return action
    return None

def _oxc_command_line_impl(ctx):
    env = analysistest.begin(ctx)
    oxc_actions = [
        action
        for action in analysistest.target_actions(env)
        if action.mnemonic == "OxcCompile"
    ]

    # One invocation, not one per directory: --strip-dir-prefix is the package,
    # so a source's depth below it survives into --out-dir.
    asserts.equals(env, 1, len(oxc_actions), "OxcCompile actions")
    if len(oxc_actions) != 1:
        return analysistest.end(env)

    argv = oxc_actions[0].argv
    out_dir = argv[argv.index("--out-dir") + 1]
    asserts.true(
        env,
        out_dir.endswith("/tests/compiler_options/analysis"),
        "--out-dir is the package's bin directory: " + out_dir,
    )
    asserts.equals(
        env,
        "tests/compiler_options/analysis",
        argv[argv.index("--strip-dir-prefix") + 1],
        "--strip-dir-prefix",
    )
    return analysistest.end(env)

oxc_command_line_test = analysistest.make(_oxc_command_line_impl)

def _forwarded_files_impl(ctx):
    env = analysistest.begin(ctx)
    target = analysistest.target_under_test(env)

    # DefaultInfo is this target's own outputs. A dep's .css reaches a consumer
    # through CssInfo, which is the provider that says what it is.
    default_files = [f.basename for f in target[DefaultInfo].files.to_list()]
    asserts.equals(
        env,
        [],
        [name for name in default_files if name.endswith(".css")],
        "DefaultInfo carries a dep's CSS: " + str(default_files),
    )
    asserts.true(
        env,
        "styled.js" in default_files,
        "DefaultInfo is missing this target's own output: " + str(default_files),
    )

    # ts_compile produces no CSS, so its direct set is empty and the closure
    # travels in the transitive one.
    asserts.equals(env, [], target[CssInfo].css_files.to_list(), "CssInfo.css_files")
    asserts.equals(
        env,
        ["styles.css"],
        [f.basename for f in target[CssInfo].transitive_css_files.to_list()],
        "CssInfo.transitive_css_files",
    )
    return analysistest.end(env)

forwarded_files_test = analysistest.make(_forwarded_files_impl)

# The options a target gets from the ruleset. Restated here rather than
# imported: the point of the assertion is that a change to _BASELINE_OPTIONS is
# a change somebody has to come and make here too.
_BASELINE_KEYS = {
    "strict": True,
    "module": "Preserve",
    "target": "es2022",
    "jsx": "react-jsx",
    "skipLibCheck": True,
    "esModuleInterop": True,
    "allowArbitraryExtensions": True,
}

def _baseline_file_impl(ctx):
    """The baseline is a file the action config extends FIRST, and it names no resolver.

    TypeScript couples moduleResolution to `module`: a layer that owns the one
    may state the other, and the baseline's `module` is beaten by any tsconfig
    that sets its own, so it leaves the resolver to tsgo to derive.
    """
    env = analysistest.begin(ctx)
    baseline = _written_file_action(env, ".tsconfig_baseline.json")
    asserts.true(env, baseline != None, "ts_compile wrote no baseline to extend")
    if baseline == None:
        return analysistest.end(env)

    opts = json.decode(baseline.content)["compilerOptions"]
    for key, want in _BASELINE_KEYS.items():
        asserts.equals(env, want, opts.get(key), key)
    asserts.equals(env, None, opts.get("moduleResolution"), "moduleResolution stays out of the baseline")
    asserts.equals(env, sorted(_BASELINE_KEYS), sorted(opts), "the baseline holds these keys and no other")
    return analysistest.end(env)

baseline_file_test = analysistest.make(_baseline_file_impl)

def _fails_with(message, config_settings = {}):
    def _impl(ctx):
        env = analysistest.begin(ctx)
        asserts.expect_failure(env, message)
        return analysistest.end(env)

    return analysistest.make(_impl, expect_failure = True, config_settings = config_settings)

declaration_map_without_tsgo_test = _fails_with(
    "--//ts:declaration_map needs the tsgo declaration emit",
    config_settings = {
        str(Label("//ts:declarations")): "oxc",
        str(Label("//ts:declaration_map")): True,
    },
)
mixed_source_roots_test = _fails_with("different roots, and one declaration emit has one rootDir")
jsx_source_test = _fails_with("which oxc has no output extension for")

def _fake_file(path):
    return struct(path = path, dirname = path.rsplit("/", 1)[0])

# `shipped` and `types_shipped` are package-relative declaration paths under the
# package's root and its paired @types package's, as declaration_files has both.
def _fake_npm_package(name, root = None, subpaths = None, ambient = None, shipped = [], types_shipped = []):
    package_root = "npm/" + name
    types_root = "npm/@types/" + name
    return struct(
        package_name = name,
        package_root = package_root,
        types_package_dir = _fake_file(types_root + "/package.json") if types_shipped else None,
        declaration_files = depset(
            [_fake_file(package_root + "/" + p) for p in shipped] +
            [_fake_file(types_root + "/" + p) for p in types_shipped],
        ),
        exports_types_file = root,
        subpath_types = subpaths or {},
        ambient_types_file = ambient,
    )

def _path(f):
    return f.path if f else None

def _types_entry_package_ref_impl(ctx):
    env = unittest.begin(ctx)

    asserts.equals(env, "vite/client", types_entry_package_ref("vite/client"), "a package subpath")
    asserts.equals(env, "node", types_entry_package_ref("node"), "a bare package name")

    # Gazelle trims before it reads the same shapes, so these three are entries
    # it writes a dep for and this side has to spend it.
    asserts.equals(env, "vite/client", types_entry_package_ref(" vite/client "), "a padded entry")
    asserts.equals(env, "node", types_entry_package_ref("\tnode\n"), "a tab- and newline-padded entry")

    # A path: no dep resolves one, so no dep is missing when one does not --
    # `types_entry_declaration` takes the two shapes a label answers for. One
    # assertion per shape, none of them reachable through another: a `.d.ts`
    # suffix would exempt an absolute declaration file whatever its prefix said.
    asserts.equals(env, "", types_entry_package_ref("./typings"), "a package-relative directory")
    asserts.equals(env, "", types_entry_package_ref("../sibling/typings"), "a directory above the package")
    asserts.equals(env, "", types_entry_package_ref("/abs/typings"), "an absolute directory")
    asserts.equals(env, "", types_entry_package_ref("vendor/local.d.ts"), "a declaration file")
    asserts.equals(env, "", types_entry_package_ref("vendor/local.d.mts"), "a .d.mts declaration file")
    asserts.equals(env, "", types_entry_package_ref("vendor/local.d.cts"), "a .d.cts declaration file")

    # Nothing names nothing: a blank entry trims away to no package at all,
    # which is what Gazelle writes no dep for.
    asserts.equals(env, "", types_entry_package_ref(""), "an empty entry")
    asserts.equals(env, "", types_entry_package_ref("   "), "a blank entry")

    return unittest.end(env)

types_entry_package_ref_test = unittest.make(_types_entry_package_ref_impl)

def _types_entry_declaration_impl(ctx):
    env = unittest.begin(ctx)

    # The two shapes TypeScript resolves relative to the config's own directory,
    # which is the half of the file-shaped entries this rule can stage.
    asserts.equals(env, "./local.d.ts", types_entry_declaration("./local.d.ts"), "a package-relative declaration")
    asserts.equals(env, "./compile.d.mts", types_entry_declaration("./compile.d.mts"), "tsc's name for a .mjs module's declaration")
    asserts.equals(env, "./shim.d.cts", types_entry_declaration("./shim.d.cts"), "and for a .cjs module's")
    asserts.equals(env, "../../worker-configuration.d.ts", types_entry_declaration(" ../../worker-configuration.d.ts "), "a padded entry above the package")

    # A directory: which declaration under it TypeScript picks is a question
    # only reading the directory answers.
    asserts.equals(env, "", types_entry_declaration("./typings"), "a package-relative directory")

    # Not relative by TypeScript's own test, so it is a typeRoots lookup the
    # compiler performs at action time -- and one nothing rebases either.
    asserts.equals(env, "", types_entry_declaration("vendor/local.d.ts"), "a bare path")
    asserts.equals(env, "", types_entry_declaration("/abs/local.d.ts"), "an absolute declaration")

    asserts.equals(env, "", types_entry_declaration("vite/client"), "a package subpath")
    asserts.equals(env, "", types_entry_declaration("   "), "a blank entry")

    return unittest.end(env)

types_entry_declaration_test = unittest.make(_types_entry_declaration_impl)

def _types_entry_file_impl(ctx):
    env = unittest.begin(ctx)

    vite = _fake_npm_package("vite", root = "index.d.ts", subpaths = {"./client": "client.d.ts"})
    asserts.equals(env, "index.d.ts", types_entry_file("vite", vite), "the package itself")
    asserts.equals(env, "client.d.ts", types_entry_file("vite/client", vite), "an exports subpath")
    asserts.equals(env, "client.d.ts", types_entry_file(" vite/client ", vite), "a padded exports subpath")
    asserts.equals(env, None, types_entry_file("", vite), "an empty entry")
    asserts.equals(env, None, types_entry_file("./client.d.ts", vite), "a file, not this package")
    asserts.equals(env, None, types_entry_file("vite/nope", vite), "a subpath it does not designate")
    asserts.equals(env, None, types_entry_file("vitest", vite), "a longer name with the same prefix")

    # A subpath the manifest leaves unnamed resolves to what the package ships
    # there, `<sub>.d.ts` ahead of `<sub>/index.d.ts`, as tsc's node_modules walk reads.
    workers = _fake_npm_package(
        "@cloudflare/workers-types",
        root = "index.d.ts",
        shipped = ["index.d.ts", "2023-07-01/index.d.ts", "experimental/index.d.ts"],
    )
    asserts.equals(
        env,
        "npm/@cloudflare/workers-types/2023-07-01/index.d.ts",
        _path(types_entry_file("@cloudflare/workers-types/2023-07-01", workers)),
        "a shipped subpath directory",
    )
    asserts.equals(env, None, types_entry_file("@cloudflare/workers-types/2023-07-1", workers), "a directory it does not ship")
    asserts.equals(env, None, types_entry_file("@cloudflare/workers-types/", workers), "the package with a trailing slash")

    shapes = _fake_npm_package(
        "shapes",
        shipped = ["both.d.ts", "both/index.d.ts", "modern.d.mts", "nested/deep.d.ts", "dist/elsewhere.d.ts"],
    )
    asserts.equals(env, "npm/shapes/both.d.ts", _path(types_entry_file("shapes/both", shapes)), "a file ahead of the directory of the same name")
    asserts.equals(env, None, types_entry_file("shapes/modern", shapes), "a .d.mts, which tsc reads for no `types` entry")
    asserts.equals(env, "npm/shapes/nested/deep.d.ts", _path(types_entry_file("shapes/nested/deep", shapes)), "a subpath more than one directory down")
    asserts.equals(env, None, types_entry_file("shapes/elsewhere", shapes), "a declaration shipped somewhere else under the root")
    asserts.equals(env, None, types_entry_file("shapes/nested", shapes), "a directory with no index")

    # Where the manifest has an answer it is the answer; the walk over shipped
    # files is for the subpaths it is silent about.
    designated = _fake_npm_package(
        "designated",
        subpaths = {"./client": "dist/client.d.ts"},
        shipped = ["client.d.ts", "dist/client.d.ts"],
    )
    asserts.equals(env, "dist/client.d.ts", types_entry_file("designated/client", designated), "the manifest's answer over a same-named shipped file")

    # typesVersions is not carried, so a subpath its map rewrites (tsc reads
    # dist/types/ts3.6/polyfill.d.ts here) answers with the file at the spelled path.
    polyfill = _fake_npm_package(
        "web-streams-polyfill",
        root = "dist/types/polyfill.d.ts",
        shipped = ["dist/types/polyfill.d.ts", "dist/types/ponyfill.d.ts", "dist/types/ts3.6/polyfill.d.ts", "dist/types/ts3.6/ponyfill.d.ts"],
    )
    asserts.equals(
        env,
        "npm/web-streams-polyfill/dist/types/polyfill.d.ts",
        _path(types_entry_file("web-streams-polyfill/dist/types/polyfill", polyfill)),
        "a subpath the manifest's typesVersions rewrites: the shipped file at the spelled path",
    )

    # `react/jsx-runtime` is @types/react's jsx-runtime.d.ts, a file under the
    # paired @types package: the last place tsc's node_modules walk reads.
    react = _fake_npm_package("react", shipped = ["own.d.ts"], types_shipped = ["index.d.ts", "jsx-runtime.d.ts"])
    asserts.equals(env, "npm/@types/react/jsx-runtime.d.ts", _path(types_entry_file("react/jsx-runtime", react)), "a file only the paired @types package ships")
    asserts.equals(env, "npm/react/own.d.ts", _path(types_entry_file("react/own", react)), "a file only the runtime package ships")

    # With a candidate in both roots, the @types package's directory index is
    # TypeScript's first answer (typeRoots), its file the last.
    twice = _fake_npm_package(
        "twice",
        shipped = ["both.d.ts", "both/index.d.ts", "file.d.ts", "dir/index.d.ts"],
        types_shipped = ["both/index.d.ts", "both.d.ts", "file.d.ts", "dir.d.ts"],
    )
    asserts.equals(env, "npm/@types/twice/both/index.d.ts", _path(types_entry_file("twice/both", twice)), "the @types package's directory index ahead of all the package ships")
    asserts.equals(env, "npm/twice/file.d.ts", _path(types_entry_file("twice/file", twice)), "the package's file ahead of the @types package's file")
    asserts.equals(env, "npm/twice/dir/index.d.ts", _path(types_entry_file("twice/dir", twice)), "the package's directory index ahead of the @types package's file")

    scoped = _fake_npm_package(
        "@cloudflare/vitest-pool-workers",
        subpaths = {"./types": "types.d.ts"},
    )
    asserts.equals(env, None, types_entry_file("@cloudflare/vitest-pool-workers", scoped), "a scoped package designating no root")
    asserts.equals(env, "types.d.ts", types_entry_file("@cloudflare/vitest-pool-workers/types", scoped), "a scoped subpath")

    # `types = ["node"]` is @types/node: the bare name is the only one anything
    # writes, and its declarations are the package's ambient entry point.
    node = _fake_npm_package("@types/node", ambient = "node.d.ts")
    asserts.equals(env, "node.d.ts", types_entry_file("node", node), "the bare name a @types package supplies")
    asserts.equals(env, "node.d.ts", types_entry_file("@types/node", node), "the @types package under its own name")
    asserts.equals(env, None, types_entry_file("nodes", node), "a name the @types package does not supply")

    bare = _fake_npm_package("culori")
    asserts.equals(env, None, types_entry_file("culori", bare), "a package designating nothing at all")

    return unittest.end(env)

types_entry_file_test = unittest.make(_types_entry_file_impl)
