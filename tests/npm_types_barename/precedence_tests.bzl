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
load("//ts/private:tsconfig_aspect.bzl", "WorkspaceCopyInfo", "npm_key_beats", "npm_view")

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

    # :own_import declares @types/estree in its own deps, which is the case
    # where both routes exist -- and they name one installed copy. Reached
    # transitively, as in :widened_to_any, there is no `files` entry at all
    # and the `paths` key above is the only route.
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

def _editor_key_collision_impl(ctx):
    env = analysistest.begin(ctx)
    config = _editor_config(env)
    asserts.true(env, config != None, "ide_tsconfig wrote no tsconfig")
    if config == None:
        return analysistest.end(env)

    paths = config["compilerOptions"]["paths"]

    # Two entries claim `chai`: the @types/chai one from :types_only_chai, whose
    # closure holds no chai to shadow it, and chai's own from :runtime_chai.
    # npm answers `x` from node_modules/x, so chai's is the one that keeps the
    # key -- and the two are not interchangeable even here, where both reach
    # @types/chai's declarations: chai's entry is the package directory, and the
    # tsconfig ts_compile generates for :runtime_chai names a directory too.
    asserts.equals(
        env,
        ["./.bazel/npm/chai"],
        paths.get("chai"),
        "the entry installed under the key's own name keeps the key",
    )
    asserts.equals(
        env,
        ["./.bazel/npm/chai/*"],
        paths.get("chai/*"),
        "and its wildcard stays in the same package",
    )
    asserts.equals(
        env,
        None,
        paths.get("@types/chai"),
        "the @types package still gets no key of its own",
    )

    installed = {
        entry.dest: True
        for entry in analysistest.target_under_test(env)[WorkspaceCopyInfo].entries.to_list()
    }
    asserts.true(
        env,
        ".bazel/npm/chai/index.d.ts" in installed,
        "the declarations the winning key points at are installed: " +
        str([d for d in installed if "chai" in d]),
    )

    # :types_only_chai deps on @types/chai directly, so it asks for the globals
    # and the alias is in `files` -- under its own name that is a second copy of
    # the declarations the `chai` key already reaches, and each name in them is
    # then declared twice. There is exactly one copy in the program because the
    # entry names the winner's.
    asserts.equals(
        env,
        ["./.bazel/npm/chai/index.d.ts"],
        config.get("files"),
        "the ambient entry names the winner's copy",
    )

    # A `files` entry off disk is not a diagnostic in one file: tsserver reports
    # it against the config and checks nothing.
    asserts.equals(
        env,
        [],
        [f for f in config.get("files", []) if f.removeprefix("./") not in installed],
        "no `files` entry names a path nothing installs: " + str(config.get("files")),
    )

    # `files` switches off the implicit include, so the workspace's own sources
    # are in the program only while this is spelled out beside it.
    asserts.equals(env, ["**/*"], config.get("include"), "and `include` is spelled out")
    return analysistest.end(env)

editor_key_collision_test = analysistest.make(_editor_key_collision_impl)

def _sources(paths, ambient, files):
    return [struct(
        npm_paths = depset(paths),
        npm_ambient = depset(ambient),
        npm_files = depset(files),
    )]

def _ambient(name, entry):
    return struct(name = name, version = "1.0.0", entry = entry)

def _installed(name, dest):
    return struct(name = name, version = "1.0.0", dest = dest, file = None)

def _npm_ambient_repoint_impl(ctx):
    env = unittest.begin(ctx)

    alias = _entry("chai", "@types/chai")
    runtime = _entry("chai", "chai")

    # The shape :key_collision_ide_tsconfig has: chai ships no declarations of
    # its own, so its entry stages @types/chai's file layout under `chai` and
    # the losing entry has a copy to be repointed at.
    _, repointed, _ = npm_view(_sources(
        [alias, runtime],
        [_ambient("@types/chai", "index.d.ts")],
        [_installed("chai", "index.d.ts"), _installed("@types/chai", "index.d.ts")],
    ))
    asserts.equals(
        env,
        [("@types/chai", struct(dir = "chai", entry = "index.d.ts"))],
        repointed,
        "the losing entry names the winner's copy",
    )

    # The winner shipping its own declarations elsewhere is the shape with no
    # copy to name: emitting `chai/index.d.ts` anyway would fail the program.
    _, dropped, _ = npm_view(_sources(
        [alias, runtime],
        [_ambient("@types/chai", "index.d.ts")],
        [_installed("chai", "dist/chai.d.ts"), _installed("@types/chai", "index.d.ts")],
    ))
    asserts.equals(env, [], dropped, "and drops rather than dangle")

    # A name that won its key is untouched, and so is one that claimed nothing:
    # an `@types/x` shadowed within one closure never reaches npm_paths at all,
    # and its declarations are its own rather than a second copy of anything.
    _, kept, _ = npm_view(_sources(
        [alias],
        [_ambient("@types/chai", "index.d.ts")],
        [_installed("@types/chai", "index.d.ts")],
    ))
    asserts.equals(
        env,
        [("@types/chai", struct(dir = "@types/chai", entry = "index.d.ts"))],
        kept,
        "a winning entry keeps its own directory",
    )
    _, unclaimed, _ = npm_view(_sources(
        [runtime],
        [_ambient("@types/chai", "index.d.ts")],
        [_installed("chai", "index.d.ts"), _installed("@types/chai", "index.d.ts")],
    ))
    asserts.equals(
        env,
        [("@types/chai", struct(dir = "@types/chai", entry = "index.d.ts"))],
        unclaimed,
        "and so does one that claimed no key",
    )

    # A winner that names its own `types` entry is not serving the loser's
    # declarations, even when it happens to ship a file where the loser's sat --
    # a legacy root stub beside an `exports` entry under dist/ is an ordinary
    # npm shape. Repointing at that stub would swap the globals for an
    # unrelated file with no diagnostic.
    _, stub, _ = npm_view(_sources(
        [alias, _entry("chai", "chai", entry = "dist/index.d.ts", is_file = True)],
        [_ambient("@types/chai", "index.d.ts")],
        [
            _installed("chai", "dist/index.d.ts"),
            _installed("chai", "index.d.ts"),
            _installed("@types/chai", "index.d.ts"),
        ],
    ))
    asserts.equals(env, [], stub, "a legacy stub at the loser's path is not the winner's copy")

    # A collision is per key. An `@types/x` that loses the bare `x` and wins a
    # subpath key of its own has still lost the bare one, and its copy under
    # that key is still duplicated.
    _, subpath, _ = npm_view(_sources(
        [
            alias,
            runtime,
            _entry("chai/register-should", "@types/chai", entry = "register-should.d.ts", is_file = True),
        ],
        [_ambient("@types/chai", "index.d.ts")],
        [
            _installed("chai", "index.d.ts"),
            _installed("chai", "register-should.d.ts"),
            _installed("@types/chai", "index.d.ts"),
            _installed("@types/chai", "register-should.d.ts"),
        ],
    ))
    asserts.equals(
        env,
        [("@types/chai", struct(dir = "chai", entry = "index.d.ts"))],
        subpath,
        "winning a subpath key does not exempt the bare key it lost",
    )

    return unittest.end(env)

npm_ambient_repoint_test = unittest.make(_npm_ambient_repoint_impl)

def _entry(key, name, entry = "", is_file = False):
    return struct(key = key, name = name, version = "1.0.0", entry = entry, is_file = is_file)

def _npm_key_beats_impl(ctx):
    env = unittest.begin(ctx)

    runtime = _entry("chai", "chai")
    alias = _entry("chai", "@types/chai")

    asserts.true(env, npm_key_beats(runtime, None), "the first entry for a key takes it")
    asserts.true(env, npm_key_beats(runtime, alias), "a runtime package displaces an alias")
    asserts.false(env, npm_key_beats(alias, runtime), "and an alias does not displace it")

    # The order the entries arrive in is the order their package names sort in,
    # and `@types/x` sorts before `x`: without the rule above the alias would
    # simply be first and stay.
    asserts.true(env, alias.name < runtime.name, "the alias is the one that sorts first")

    # Two aliases for one key cannot both be right either, so the answer is at
    # least stable: the lower package name.
    other = _entry("chai", "@types/chai-extra")
    asserts.true(env, npm_key_beats(alias, other), "between two aliases, the lower name")
    asserts.false(env, npm_key_beats(other, alias), "and not the other way round")

    return unittest.end(env)

npm_key_beats_test = unittest.make(_npm_key_beats_impl)

def _editor_transitive_types_impl(ctx):
    env = analysistest.begin(ctx)
    config = _editor_config(env)
    asserts.true(env, config != None, "ide_tsconfig wrote no tsconfig")
    if config == None:
        return analysistest.end(env)

    # The `paths` key is here for a package no target in this closure declares:
    # rollup's dist/rollup.d.ts is what says `from "estree"`.
    asserts.equals(
        env,
        ["./.bazel/npm/@types/estree/index.d.ts"],
        config["compilerOptions"]["paths"].get("estree"),
        "a transitively reached @types package still answers the name it types",
    )

    # And `files` names nothing from it. That array is built from what each
    # reached target declares in its own deps, so the two routes a @types/*
    # package can take are not both available for every one of them -- the claim
    # that they always are is what this fixture disproves.
    asserts.equals(
        env,
        [],
        [entry for entry in config.get("files", []) if "estree" in entry],
        "and reaches the program through no `files` entry: " + str(config.get("files")),
    )
    asserts.true(
        env,
        ".bazel/npm/@types/estree/index.d.ts" in [
            entry.dest
            for entry in analysistest.target_under_test(env)[WorkspaceCopyInfo].entries.to_list()
        ],
        "so the paths entry is what installs the declarations",
    )
    return analysistest.end(env)

editor_transitive_types_test = analysistest.make(_editor_transitive_types_impl)
