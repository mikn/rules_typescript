"""What the bare-name key a @types/* package gets is allowed to displace.

The build targets next to this file prove the key resolves. These prove it
resolves to the right thing: npm answers `x` from `node_modules/x` and falls
back to `node_modules/@types/x` only when the first ships no declarations, and a
path_alias is the consumer naming the module outright.

Each question is asked twice, once of the tsconfig ts_compile hands the compiler
and once of the tsconfig `bazel run //:refresh_tsconfig` installs for an editor.
Two code paths generate those from one graph, and only the first of them used to
resolve a @types/* package at all.
"""

load("@bazel_skylib//lib:unittest.bzl", "analysistest", "asserts", "unittest")
load("//ts/private:ts_compile.bzl", "types_package_alias")
load("//ts/private:tsconfig_aspect.bzl", "WorkspaceCopyInfo")

_TYPES = "+npm+npm__types_"

def _paths_of(env):
    for action in analysistest.target_actions(env):
        outputs = action.outputs.to_list()
        if len(outputs) == 1 and outputs[0].basename.endswith(".tsconfig.json"):
            return json.decode(action.content)["compilerOptions"]["paths"]
    return None

def _types_alias_precedence_impl(ctx):
    env = analysistest.begin(ctx)
    paths = _paths_of(env)
    asserts.true(env, paths != None, "ts_compile generated no tsconfig")
    if paths == None:
        return analysistest.end(env)

    # No runtime package of that name, and the path_alias claims it anyway.
    # Asserted as "no value names the @types package" rather than as the exact
    # list: how many trees an alias expands to is path_aliases' business.
    alias_value = paths.get("estree")
    asserts.true(env, alias_value != None, "the path_alias key is gone")
    asserts.true(
        env,
        alias_value and alias_value[0].endswith("/tests/npm_types_barename"),
        "the path_alias no longer resolves to its own directory: {}".format(alias_value),
    )
    asserts.true(
        env,
        alias_value and not [v for v in alias_value if _TYPES in v],
        "a path_alias must outrank the @types package that would take the name: {}".format(alias_value),
    )
    asserts.true(
        env,
        paths.get("@types/estree") != None,
        "the @types package keeps its own key",
    )

    # @babel/core, /generator, /template and /traverse publish no .d.ts, so
    # their declarations are only in @types/babel__*; @babel/types and
    # @babel/parser publish their own, and nothing may displace those.
    for name in ("core", "generator", "template", "traverse"):
        value = paths.get("@babel/" + name)
        asserts.true(env, value != None, "@babel/{} has no paths entry".format(name))
        asserts.true(
            env,
            value and _TYPES + "babel__" + name in value[0],
            "@babel/{} resolves to {}, not its @types package".format(name, value),
        )
    for name in ("parser", "types"):
        value = paths.get("@babel/" + name)
        asserts.true(env, value != None, "@babel/{} has no paths entry".format(name))
        asserts.true(
            env,
            value and _TYPES not in value[0],
            "@babel/{} resolves to {}, not to what it publishes itself".format(name, value),
        )
    return analysistest.end(env)

def _types_package_root_impl(ctx):
    env = analysistest.begin(ctx)
    paths = _paths_of(env)
    asserts.true(env, paths != None, "ts_compile generated no tsconfig")
    if paths == None:
        return analysistest.end(env)

    # @types/culori keeps `all/`, `css/` and `fn/` beside its index. Naming one
    # of those instead of the package puts `culori` inside a single module and
    # leaves every subpath unresolvable, so the key has to end at the package.
    for key in ("culori", "culori/*"):
        value = paths.get(key)
        asserts.true(env, value != None, "{} has no paths entry".format(key))
        if value == None:
            continue
        directory = value[0][:-len("/*")] if key.endswith("/*") else value[0]
        asserts.true(
            env,
            directory.split("/")[-1].endswith("npm__types_culori__2_1_1"),
            "{} resolves inside the @types package, not to it: {}".format(key, value),
        )
    return analysistest.end(env)

types_package_root_test = analysistest.make(_types_package_root_impl)

types_alias_precedence_test = analysistest.make(_types_alias_precedence_impl)

def _types_package_alias_impl(ctx):
    env = unittest.begin(ctx)

    asserts.equals(env, "estree", types_package_alias("@types/estree"), "an unscoped name")
    asserts.equals(env, None, types_package_alias("estree"), "a package that is not a @types one")
    asserts.equals(env, None, types_package_alias("@babel/core"), "a scoped package of its own")

    # DefinitelyTyped mangles the scope separator, and only the first one: the
    # types for `@a/b__c` are published as `@types/a__b__c`.
    asserts.equals(env, "@babel/core", types_package_alias("@types/babel__core"), "a mangled scope")
    asserts.equals(env, "@a/b__c", types_package_alias("@types/a__b__c"), "a name holding the separator")

    return unittest.end(env)

types_package_alias_test = unittest.make(_types_package_alias_impl)

# ─── The same questions, asked of the tsconfig an editor reads ───────────────

def _editor_config(env):
    for action in analysistest.target_actions(env):
        outputs = action.outputs.to_list()
        if len(outputs) == 1 and outputs[0].basename.endswith(".json"):
            return json.decode(action.content)
    return None

def _editor_types_alias_impl(ctx):
    env = analysistest.begin(ctx)
    config = _editor_config(env)
    asserts.true(env, config != None, "ide_tsconfig wrote no tsconfig")
    if config == None:
        return analysistest.end(env)

    paths = config["compilerOptions"]["paths"]

    # @types/estree has no runtime package to be reached under, so before the
    # key existed the editor had no answer for `estree` at all while the build
    # resolved it -- the same file red in one and clean in the other.
    asserts.equals(
        env,
        ["./.bazel/npm/@types/estree/index.d.ts"],
        paths.get("estree"),
        "the name @types/estree types resolves to its entry point",
    )
    asserts.equals(
        env,
        ["./.bazel/npm/@types/estree/*"],
        paths.get("estree/*"),
        "and its subpaths reach the same package",
    )

    # The install directory is the package's own, which is where the `files`
    # entry already puts it: one copy under npm_dir answers both.
    asserts.equals(
        env,
        ["./.bazel/npm/@types/estree/index.d.ts"],
        config.get("files"),
        "the ambient entry point is the same installed file",
    )
    asserts.equals(
        env,
        None,
        paths.get("@types/estree"),
        "the package's own name gets no key, since no import writes it",
    )
    return analysistest.end(env)

editor_types_alias_test = analysistest.make(_editor_types_alias_impl)

def _editor_types_precedence_impl(ctx):
    env = analysistest.begin(ctx)
    config = _editor_config(env)
    asserts.true(env, config != None, "ide_tsconfig wrote no tsconfig")
    if config == None:
        return analysistest.end(env)

    paths = config["compilerOptions"]["paths"]

    # The mangled scope: @babel/core publishes no declarations, so
    # @types/babel__core answers its name. @babel/types publishes its own and
    # keeps them -- a guard, not the precedence assertion, since this lockfile
    # has no @types package competing for that name. What happens when one does
    # is //tests/lsp:transitive_types_pairing_test, where @types/chai is in the
    # closure and chai already carries its declarations.
    asserts.equals(
        env,
        ["./.bazel/npm/@types/babel__core/index.d.ts"],
        paths.get("@babel/core"),
        "a mangled scope is demangled into the name it types",
    )
    value = paths.get("@babel/types")
    asserts.true(env, value != None, "@babel/types has no entry")
    asserts.true(
        env,
        value and "@types" not in value[0],
        "@babel/types resolves to {}, not to what it publishes itself".format(value),
    )

    installed = [
        entry.dest
        for entry in analysistest.target_under_test(env)[WorkspaceCopyInfo].entries.to_list()
    ]
    asserts.true(
        env,
        ".bazel/npm/@types/babel__core/index.d.ts" in installed,
        "the declarations that key points at are installed: " +
        str([d for d in installed if "babel__core" in d]),
    )
    return analysistest.end(env)

editor_types_precedence_test = analysistest.make(_editor_types_precedence_impl)

def _editor_alias_beats_types_impl(ctx):
    env = analysistest.begin(ctx)
    config = _editor_config(env)
    asserts.true(env, config != None, "ide_tsconfig wrote no tsconfig")
    if config == None:
        return analysistest.end(env)

    paths = config["compilerOptions"]["paths"]
    value = paths.get("estree")
    asserts.true(env, value != None, "the path_alias key is gone")
    asserts.equals(
        env,
        ["./tests/npm_types_barename/index"],
        value,
        "a path_alias must outrank the @types package that would take the name",
    )
    return analysistest.end(env)

editor_alias_beats_types_test = analysistest.make(_editor_alias_beats_types_impl)
