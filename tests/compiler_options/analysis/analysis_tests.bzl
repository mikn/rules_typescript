"""Analysis-time coverage for ts_compile.

Three kinds of assertion live here, none of which a build test can make:

  - what the rule *tells* the compilers -- the generated tsconfig and the oxc
    command line, read straight out of the registered actions;
  - that every guard fails, with the message that names the way out;
  - what a helper behind either one answers, called directly.

A guard's target is tagged manual so that `bazel build //...` does not try to
analyse it and stop on the very failure being asserted.
"""

load("@bazel_skylib//lib:unittest.bzl", "analysistest", "asserts", "unittest")
load("//ts:defs.bzl", "CssInfo")
load("//ts/private:ts_compile.bzl", "types_entry_declaration", "types_entry_file", "types_entry_package_ref")
load("//ts/private:ts_test.bzl", "test_compiler_options_json")

def _written_file_action(env, suffix):
    for action in analysistest.target_actions(env):
        outputs = action.outputs.to_list()
        if len(outputs) == 1 and outputs[0].basename.endswith(suffix):
            return action
    return None

def _generated_tsconfig_impl(ctx):
    env = analysistest.begin(ctx)
    action = _written_file_action(env, ".tsconfig.json")
    asserts.true(env, action != None, "ts_compile generated no tsconfig")
    if action == None:
        return analysistest.end(env)

    config = json.decode(action.content)
    opts = config["compilerOptions"]

    # A JavaScript src is in `include`, so tsgo has to be told to read it.
    asserts.equals(env, True, opts.get("allowJs"), "allowJs")
    asserts.equals(env, True, opts.get("checkJs"), "checkJs reached tsgo")
    asserts.equals(env, True, opts.get("declarationMap"), "declarationMap")

    # The declarations land where Bazel declared them: alongside the .js in the
    # package's bin directory, at the depth rootDir gives them.
    asserts.equals(env, ".", opts["outDir"], "outDir")
    asserts.equals(env, opts["outDir"], opts["declarationDir"], "declarationDir")
    asserts.true(
        env,
        opts["rootDir"].endswith("/tests/compiler_options/analysis"),
        "rootDir is the package: " + opts["rootDir"],
    )

    # include carries every src at its own depth -- the nested one included.
    nested = [entry for entry in config["include"] if entry.endswith("/nested/leaf.ts")]
    asserts.equals(env, 1, len(nested), "include entry for the nested source: " + str(config["include"]))
    return analysistest.end(env)

generated_tsconfig_test = analysistest.make(_generated_tsconfig_impl)

def _suffixes(entries):
    """Each `paths` entry with the module's root cut off, whichever root it is."""
    return [entry.split("/packages/pulse")[-1].removeprefix("/") for entry in entries]

def _ups(entry):
    return len([part for part in entry.split("/") if part == ".."])

def _module_exports_paths_impl(ctx):
    env = analysistest.begin(ctx)
    action = _written_file_action(env, ".tsconfig.json")
    asserts.true(env, action != None, "ts_compile generated no tsconfig")
    if action == None:
        return analysistest.end(env)

    paths = json.decode(action.content)["compilerOptions"]["paths"]

    # The subpath the member enumerates, which is four directories from its root
    # and nowhere near the `<root>/button` the wildcard entry would have offered.
    # Two entries per declaration, one for each of the module's roots, then the
    # wildcard expansion as a fallback -- so a manifest that names a file this
    # build does not produce leaves the subpath resolving as it did before.
    # .get, not indexing: a missing key is the very failure under test, and an
    # analysistest that dies on it aborts the whole package's analysis instead of
    # reporting which assertion went.
    button = paths.get("pulse/button", [])
    asserts.equals(
        env,
        [
            "components/controls/button/index.d.ts",
            "components/controls/button/index.d.ts",
            "button",
            "button",
        ],
        _suffixes(button),
        "pulse/button: " + str(button),
    )

    # The generated declarations come first. Both roots are relative to a
    # tsconfig that sits under bazel-bin, so the one that is also under bazel-bin
    # is the nearer of the two.
    asserts.true(
        env,
        len(button) > 1 and _ups(button[0]) < _ups(button[1]),
        "the declaration root is not first: " + str(button[:2]),
    )

    # `exports` names `types` before `default`, and a resolver reads the map in
    # its own key order. The condition that designates declarations wins, and the
    # other is kept behind it rather than dropped.
    asserts.equals(
        env,
        ["entry.d.ts", "dist/entry.d.ts", "entry.d.ts", "dist/entry.d.ts", "index.d.ts", "index.d.ts", "", ""],
        _suffixes(paths.get("pulse", [])),
        "pulse: " + str(paths.get("pulse")),
    )

    # A wildcard subpath becomes a wildcard pattern, and keeps its own key: a
    # longer pattern prefix wins over `pulse/*`, which stays for everything the
    # manifest does not name. The directory it points into is not the one the
    # specifier names, which is what the wildcard entry cannot guess.
    asserts.equals(
        env,
        ["styles/tokens/*.d.ts", "styles/tokens/*.d.ts", "tokens/*", "tokens/*"],
        _suffixes(paths.get("pulse/tokens/*", [])),
        "pulse/tokens/*: " + str(paths.get("pulse/tokens/*")),
    )
    asserts.equals(
        env,
        ["*", "*"],
        _suffixes(paths.get("pulse/*", [])),
        "pulse/*: " + str(paths.get("pulse/*")),
    )

    # `"./internal/*": null` is not exported and designates nothing; `theme.css`
    # is not something a compiler emits a declaration from. Neither gets a key,
    # so both keep resolving through `pulse/*` -- `paths` says where a name
    # lives, and what a target may import is the strict-deps check's answer.
    asserts.equals(
        env,
        [],
        [key for key in paths if key.startswith("pulse/internal") or key.endswith(".css")],
        "a subpath that designates no declaration got an entry: " + str(sorted(paths)),
    )
    return analysistest.end(env)

module_exports_paths_test = analysistest.make(_module_exports_paths_impl)

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

def _exec_root_source_impl(ctx):
    env = analysistest.begin(ctx)
    action = _written_file_action(env, ".tsconfig.json")
    asserts.true(env, action != None, "ts_compile generated no tsconfig")
    if action == None:
        return analysistest.end(env)

    opts = json.decode(action.content)["compilerOptions"]

    # The exec root is a source root like any other. Read as a boolean it is
    # indistinguishable from "no root dir", and rootDir then points at the bin
    # directory the tsconfig sits in -- no source is under that, so tsgo writes
    # the declarations somewhere Bazel never declared.
    asserts.true(
        env,
        opts["rootDir"].startswith("../") and not opts["rootDir"].endswith("/bin"),
        "rootDir climbs out to the exec root: " + opts["rootDir"],
    )
    asserts.equals(env, ".", opts["outDir"], "outDir")
    return analysistest.end(env)

exec_root_source_test = analysistest.make(_exec_root_source_impl)

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

def _quoted_option_impl(ctx):
    env = analysistest.begin(ctx)
    action = _written_file_action(env, ".tsconfig.json")
    asserts.true(env, action != None, "ts_compile generated no tsconfig")
    if action == None:
        return analysistest.end(env)

    # json.decode is the assertion: a tsconfig assembled by string
    # concatenation does not survive a value with a quote and a backslash in it.
    opts = json.decode(action.content)["compilerOptions"]
    asserts.equals(env, "a\"b\\c", opts["jsxFactory"], "jsxFactory round-tripped")
    return analysistest.end(env)

quoted_option_test = analysistest.make(_quoted_option_impl)

# The options a target gets from the ruleset in either mode. Restated here
# rather than imported: the point of the assertion is that a change to
# _BASELINE_OPTIONS is a change somebody has to come and make here too.
_BASELINE_KEYS = {
    "strict": True,
    "module": "Preserve",
    "skipLibCheck": True,
    "esModuleInterop": True,
}

# The fifth, which TypeScript couples to `module`: only a layer that owns the
# `module` it fits may state it, so it is in the generated file without a
# tsconfig and in neither file with one.
_DERIVED_KEY = "moduleResolution"
_DERIVED_VALUE = "Bundler"

def _zero_config_baseline_impl(ctx):
    """With no `tsconfig`, the baseline is in the generated file itself."""
    env = analysistest.begin(ctx)
    action = _written_file_action(env, ".tsconfig.json")
    asserts.true(env, action != None, "ts_compile generated no tsconfig")
    if action == None:
        return analysistest.end(env)

    config = json.decode(action.content)
    asserts.equals(env, None, config.get("extends"), "no tsconfig, so nothing to extend")
    for key, want in _BASELINE_KEYS.items():
        asserts.equals(env, want, config["compilerOptions"].get(key), key)

    # This layer is the one that owns `module`, so it is the one place the
    # derived partner may be asserted.
    asserts.equals(env, _DERIVED_VALUE, config["compilerOptions"].get(_DERIVED_KEY), _DERIVED_KEY)
    return analysistest.end(env)

zero_config_baseline_test = analysistest.make(_zero_config_baseline_impl)

def _derived_resolution_impl(ctx):
    """A `module` of the target's own leaves the derivation to tsgo.

    The ruleset's `module` is gone the moment compiler_options names one, and a
    `moduleResolution` left behind belongs to a module that is no longer there:
    Bundler under NodeNext is TS5109 before tsgo reads a single source.
    """
    env = analysistest.begin(ctx)
    action = _written_file_action(env, ".tsconfig.json")
    asserts.true(env, action != None, "ts_compile generated no tsconfig")
    if action == None:
        return analysistest.end(env)

    opts = json.decode(action.content)["compilerOptions"]
    asserts.equals(env, "NodeNext", opts.get("module"), "the target's own module")
    asserts.equals(
        env,
        None,
        opts.get(_DERIVED_KEY),
        "the ruleset no longer owns `module`, so it states no resolver: " + str(opts.get(_DERIVED_KEY)),
    )
    return analysistest.end(env)

derived_resolution_test = analysistest.make(_derived_resolution_impl)

def _tsconfig_baseline_impl(ctx):
    """With a `tsconfig`, the baseline is a file that config extends FIRST.

    Naming a tsconfig has to add what the file says rather than subtract what
    the ruleset already guaranteed, and `extends` is the only place a layer can
    sit under the file: a later entry in the list overrides an earlier one, so
    the baseline reaches only the keys the user's chain never mentions.
    """
    env = analysistest.begin(ctx)
    action = _written_file_action(env, ".tsconfig.json")
    baseline = _written_file_action(env, ".tsconfig_baseline.json")
    asserts.true(env, action != None, "ts_compile generated no tsconfig")
    asserts.true(env, baseline != None, "ts_compile wrote no baseline to extend")
    if action == None or baseline == None:
        return analysistest.end(env)

    config = json.decode(action.content)
    chain = config.get("extends")
    asserts.equals(env, "list", type(chain), "extends is a list: " + str(chain))
    if type(chain) != "list" or len(chain) != 2:
        asserts.true(env, False, "extends names the baseline and the user's file: " + str(chain))
        return analysistest.end(env)
    asserts.true(
        env,
        chain[0].endswith(".tsconfig_baseline.json"),
        "the baseline is extended first, so the user's file wins: " + str(chain),
    )
    asserts.true(
        env,
        chain[1].endswith("/silent.tsconfig.json"),
        "the user's file is extended last: " + str(chain),
    )

    baseline_opts = json.decode(baseline.content)["compilerOptions"]
    for key, want in _BASELINE_KEYS.items():
        asserts.equals(env, want, baseline_opts.get(key), key)

        # In the generated file these would beat the user's tsconfig instead of
        # falling behind it.
        asserts.equals(env, None, config["compilerOptions"].get(key), key + " stays out of the generated file")

    # Neither layer may state it: under the tsconfig it would outlive the module
    # it belongs to, over it it would beat the module the tsconfig chose.
    asserts.equals(env, None, baseline_opts.get(_DERIVED_KEY), _DERIVED_KEY + " stays out of the baseline")
    asserts.equals(env, None, config["compilerOptions"].get(_DERIVED_KEY), _DERIVED_KEY + " stays out of the generated file")
    return analysistest.end(env)

tsconfig_baseline_test = analysistest.make(_tsconfig_baseline_impl)

def _fails_with(*messages):
    def _impl(ctx):
        env = analysistest.begin(ctx)
        for message in messages:
            asserts.expect_failure(env, message)
        return analysistest.end(env)

    return analysistest.make(_impl, expect_failure = True)

bazel_owned_option_test = _fails_with("compilerOptions.outDir is set by the rule")
unpaired_resolution_test = _fails_with("moduleResolution is \"nodenext\" and no module is set")
alias_into_output_tree_test = _fails_with("points into the output tree")
alias_without_inputs_test = _fails_with("where none of this target's inputs live")
rejected_tsgo_arg_test = _fails_with("Only flags that report on the program are allowed")
declaration_map_without_tsgo_test = _fails_with("declaration_map on")
mixed_source_roots_test = _fails_with("different roots, and one declaration emit has one rootDir")
jsx_source_test = _fails_with("which oxc has no output extension for")

def _requested_types_impl(ctx):
    env = analysistest.begin(ctx)
    action = _written_file_action(env, ".tsconfig.json")
    asserts.true(env, action != None, "ts_compile generated no tsconfig")
    if action == None:
        return analysistest.end(env)

    # The padded subpath entry is the one thing here that only this resolution
    # puts in `files`: an @types/* dep's entry point arrives from the dep edge
    # whether or not `types` names it, and an untrimmed entry resolved to
    # nothing. Analysis reaching this assertion at all is the rest of the
    # coverage -- the guard fails the target for any of the three entries it
    # cannot resolve.
    files = json.decode(action.content).get("files", [])
    asserts.true(
        env,
        [f for f in files if "npm__vite__" in f and f.endswith("/client.d.ts")],
        "the vite/client declaration is not in `files`: {}".format(files),
    )
    return analysistest.end(env)

requested_types_test = analysistest.make(_requested_types_impl)

def _types_file_entries_impl(ctx):
    env = analysistest.begin(ctx)
    action = _written_file_action(env, ".tsconfig.json")
    asserts.true(env, action != None, "ts_compile generated no tsconfig")
    if action == None:
        return analysistest.end(env)

    # An entry no dep and no label of this target's resolves is the compiler's
    # own, so each one has to survive into the config it reads -- and a blank one
    # has to reach it without either guard firing, which is this target
    # analysing at all.
    types = json.decode(action.content)["compilerOptions"]["types"]
    asserts.true(
        env,
        [entry for entry in types if entry.startswith("..") and entry.endswith("/typings")],
        "the package-relative entry, rebased onto the generated config: {}".format(types),
    )
    asserts.true(env, "/abs/typings" in types, "the absolute entry: {}".format(types))
    asserts.true(env, "vendor/local.d.ts" in types, "the declaration file entry: {}".format(types))
    asserts.true(env, "" in types, "the empty entry: {}".format(types))
    return analysistest.end(env)

types_file_entries_test = analysistest.make(_types_file_entries_impl)

def _types_declaration_file_impl(ctx):
    env = analysistest.begin(ctx)

    # The entry is a path, so what makes it resolve is the file being in the
    # sandbox: `types_srcs` is the only thing putting it there.
    inputs = []
    for action in analysistest.target_actions(env):
        if action.mnemonic == "TsgoDeclare":
            inputs = [f.short_path for f in action.inputs.to_list()]
    asserts.true(
        env,
        "tests/compiler_options/analysis/staged/ambient.d.ts" in inputs,
        "the declaration the entry names is an input of the type-check action",
    )

    action = _written_file_action(env, ".tsconfig.json")
    asserts.true(env, action != None, "ts_compile generated no tsconfig")
    if action == None:
        return analysistest.end(env)

    # Written as the path to the file it resolved to, which for a checked-in
    # file is its source-tree path from the generated config.
    types = json.decode(action.content)["compilerOptions"]["types"]
    asserts.equals(
        env,
        1,
        len([e for e in types if e.endswith("/tests/compiler_options/analysis/staged/ambient.d.ts")]),
        "the entry, written from the generated config: {}".format(types),
    )
    return analysistest.end(env)

types_declaration_file_test = analysistest.make(_types_declaration_file_impl)

def _generated_types_entry_impl(ctx):
    env = analysistest.begin(ctx)

    inputs = []
    for action in analysistest.target_actions(env):
        if action.mnemonic == "TsgoDeclare":
            inputs = [f.short_path for f in action.inputs.to_list()]
    asserts.true(
        env,
        "tests/compiler_options/analysis/generated_in_srcs.d.ts" in inputs,
        "the generated declaration the entry names is an input of the type-check action",
    )

    action = _written_file_action(env, ".tsconfig.json")
    asserts.true(env, action != None, "ts_compile generated no tsconfig")
    if action == None:
        return analysistest.end(env)

    # Written as the path to the file it resolved to. The generated config and
    # the generated declaration sit in the same bazel-out directory, so the
    # entry does not leave it -- the source-tree rebase would have.
    types = json.decode(action.content)["compilerOptions"]["types"]
    asserts.equals(env, ["./generated_in_srcs.d.ts"], types, "the entry, from the generated config")
    return analysistest.end(env)

generated_types_entry_test = analysistest.make(_generated_types_entry_impl)

def _fake_npm_package(name, root = None, subpaths = None, ambient = None):
    return struct(
        package_name = name,
        exports_types_file = root,
        subpath_types = subpaths or {},
        ambient_types_file = ambient,
    )

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

def _test_types_forwarded_impl(ctx):
    env = unittest.begin(ctx)

    folded = test_compiler_options_json(None, ["vite/client"], None)
    asserts.true(env, folded != "", "ts_test dropped `types` from the options the rule reads")
    if folded:
        asserts.equals(env, ["vite/client"], json.decode(folded).get("types"), "ts_test folds `types` for the rule to read")
    asserts.equals(env, "", test_compiler_options_json(None, None, None), "no options stays absent, not an empty object")

    # `globals` is the runtime half; the entry is the one that declares them,
    # and it lands whether or not the target asked for any `types` of its own.
    asserts.equals(
        env,
        ["vitest/globals"],
        json.decode(test_compiler_options_json(None, None, None, True)).get("types"),
        "globals alone earns the entry",
    )
    asserts.equals(
        env,
        ["vite/client", "vitest/globals"],
        json.decode(test_compiler_options_json(None, ["vite/client"], None, True)).get("types"),
        "the entry lands after the target's own, which outrank it",
    )
    asserts.equals(
        env,
        ["node", "vitest/globals"],
        json.decode(test_compiler_options_json(None, ["vite/client"], {"types": ["node"]}, True)).get("types"),
        "compiler_options wins over `types`, and the entry still lands",
    )
    asserts.equals(
        env,
        [" vitest/globals "],
        json.decode(test_compiler_options_json(None, [" vitest/globals "], None, True)).get("types"),
        "an entry the target already wrote is not written twice",
    )
    asserts.equals(
        env,
        "",
        test_compiler_options_json(None, None, None, False),
        "globals = False adds nothing",
    )

    return unittest.end(env)

test_types_forwarded_test = unittest.make(_test_types_forwarded_impl)

unresolved_type_entry_test = _fails_with(
    "compilerOptions.types entry \"vite/client\" on",
    "No dep of this target publishes \"vite\"",
)
unresolved_type_subpath_test = _fails_with(
    "designates no declarations for the subpath \"./nope\"",
    "Did you mean one of the subpaths it does designate: ./client, ./internal, ./module-runner?",
)
unresolved_type_root_test = _fails_with(
    "\"picomatch\" is a dep, but its package.json designates no declarations for the package root",
    "It designates none, so the declarations have to be named as a file",
)
unresolved_type_near_miss_test = _fails_with("Did you mean one of these deps: vitest?")
absent_type_file_test = _fails_with(
    "names \"tests/compiler_options/analysis/staged/absent.d.ts\"",
    "which no file this target stages sits at",
    "List the file in types_srcs",
)
misplaced_type_file_test = _fails_with(
    "Did you mean \"tests/compiler_options/analysis/staged/ambient.d.ts\"?",
)
unnamed_type_src_test = _fails_with(
    "types_srcs on @@//tests/compiler_options/analysis:unnamed_type_src names 'tests/compiler_options/analysis/staged/ambient.d.ts'",
    "Name it -- types = [\"./staged/ambient.d.ts\"] from this package -- or drop it.",
)
unnamed_test_type_src_test = _fails_with(
    "types_srcs on @@//tests/compiler_options/analysis:_unnamed_test_type_src_compile names 'tests/compiler_options/analysis/staged/ambient.d.ts'",
)
unresolved_test_types_test = _fails_with("compilerOptions.types entry \"vite/client\" on")
unresolved_globals_types_test = _fails_with(
    "compilerOptions.types entry \"vitest/globals\" on",
    "Add the package to deps (e.g. \"@npm//:vitest\")",
)
