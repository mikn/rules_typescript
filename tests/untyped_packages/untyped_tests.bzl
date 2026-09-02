"""Analysis-time proof of what `untyped_packages` removes, on both sides.

A target's build tsconfig and the editor's are written by different code paths
over the same graph, and every route into a program has to answer the same way:
the `paths` key an import resolves through, the `files` entry a types-only dep
reaches with no import at all, and -- in the editor -- the copy of the
declarations `bazel run //:refresh_tsconfig` installs under `.bazel/npm`.

Every assertion here comes in a pair. One target has the attribute and one does
not, and they differ in nothing else, so what the pair attributes is the
attribute rather than anything else the aspect walked.
"""

load("@bazel_skylib//lib:unittest.bzl", "analysistest", "asserts")
load("//ts/private:tsconfig_aspect.bzl", "WorkspaceCopyInfo")

_PACKAGE = "@cloudflare/workers-types"

def _compile_config(env):
    """The tsconfig ts_compile wrote for the target under test."""
    for action in analysistest.target_actions(env):
        outputs = action.outputs.to_list()
        if len(outputs) == 1 and outputs[0].basename.endswith(".tsconfig.json"):
            return json.decode(action.content)
    return None

def _editor_config(env):
    for action in analysistest.target_actions(env):
        outputs = action.outputs.to_list()
        if len(outputs) == 1 and outputs[0].basename.endswith(".json"):
            return json.decode(action.content)
    return None

def _installed(env):
    return [
        entry.dest
        for entry in analysistest.target_under_test(env)[WorkspaceCopyInfo].entries.to_list()
    ]

def _keys_for(config, package):
    paths = config.get("compilerOptions", {}).get("paths", {})
    return sorted([key for key in paths if key == package or key.startswith(package + "/")])

def _paths_omit_impl(ctx):
    env = analysistest.begin(ctx)
    config = _compile_config(env)
    asserts.true(env, config != None, "ts_compile wrote no tsconfig")
    if config == None:
        return analysistest.end(env)

    asserts.equals(
        env,
        [],
        _keys_for(config, _PACKAGE),
        "no key resolves the excluded package",
    )

    # The dep that reaches it is untouched: the exclusion is about one package,
    # not about the closure it arrived through.
    asserts.true(
        env,
        "wrangler" in config["compilerOptions"]["paths"],
        "wrangler still resolves: " + str(_keys_for(config, "wrangler")),
    )
    return analysistest.end(env)

paths_omit_untyped_test = analysistest.make(_paths_omit_impl)

def _paths_keep_impl(ctx):
    env = analysistest.begin(ctx)
    config = _compile_config(env)
    asserts.true(env, config != None, "ts_compile wrote no tsconfig")
    if config == None:
        return analysistest.end(env)

    # The same closure without the attribute: the bare name wrangler's entry
    # point imports, and the wildcard its next line's `/experimental` subpath
    # goes through -- this package designates no `exports` subpaths of its own.
    asserts.equals(
        env,
        [_PACKAGE, _PACKAGE + "/*"],
        _keys_for(config, _PACKAGE),
        "the closure resolves it without the attribute",
    )
    return analysistest.end(env)

paths_keep_untyped_test = analysistest.make(_paths_keep_impl)

def _ambient_entries(config):
    return [entry for entry in config.get("files", []) if "workers-types" in entry]

def _files_keep_impl(ctx):
    env = analysistest.begin(ctx)
    config = _compile_config(env)
    asserts.true(env, config != None, "ts_compile wrote no tsconfig")
    if config == None:
        return analysistest.end(env)

    # A types-only package in `deps` is how a target asks for its globals, and
    # `files` is the route: no import names it anywhere in direct_ambient.ts.
    asserts.true(
        env,
        len(_ambient_entries(config)) == 1,
        "its entry point is named in `files`: " + str(config.get("files")),
    )
    return analysistest.end(env)

files_keep_ambient_test = analysistest.make(_files_keep_impl)

def _files_omit_impl(ctx):
    env = analysistest.begin(ctx)
    config = _compile_config(env)
    asserts.true(env, config != None, "ts_compile wrote no tsconfig")
    if config == None:
        return analysistest.end(env)

    asserts.equals(
        env,
        [],
        _ambient_entries(config),
        "the excluded package reaches `files` either: " + str(config.get("files")),
    )
    asserts.equals(
        env,
        [],
        _keys_for(config, _PACKAGE),
        "and no key resolves it",
    )
    return analysistest.end(env)

files_omit_ambient_test = analysistest.make(_files_omit_impl)

_ALIAS = "ms"

def _alias_keys(config):
    paths = config.get("compilerOptions", {}).get("paths", {})
    return sorted([key for key in paths if key == _ALIAS or key == _ALIAS + "/*"])

def _paths_keep_alias_impl(ctx):
    env = analysistest.begin(ctx)
    config = _compile_config(env)
    asserts.true(env, config != None, "ts_compile wrote no tsconfig")
    if config == None:
        return analysistest.end(env)

    asserts.equals(
        env,
        [_ALIAS, _ALIAS + "/*"],
        _alias_keys(config),
        "the bare name resolves without the attribute",
    )

    # And it resolves to @types/ms rather than to ms, which is the case the
    # alias exists for: dropping only the key a package owns under its own name
    # would leave this one answering for it.
    asserts.true(
        env,
        "types_ms" in config["compilerOptions"]["paths"][_ALIAS][0],
        "through the @types package: " + str(config["compilerOptions"]["paths"][_ALIAS]),
    )
    return analysistest.end(env)

paths_keep_alias_test = analysistest.make(_paths_keep_alias_impl)

def _paths_omit_alias_impl(ctx):
    env = analysistest.begin(ctx)
    config = _compile_config(env)
    asserts.true(env, config != None, "ts_compile wrote no tsconfig")
    if config == None:
        return analysistest.end(env)

    asserts.equals(
        env,
        [],
        _alias_keys(config),
        "the @types package answers the excluded name from no key",
    )

    # Its own literal key is untouched: `@types/ms` names one package and `ms`
    # names another, and this entry excluded the second. The key answers no
    # import anyone writes, which is why dropping the alias is the whole job.
    asserts.true(
        env,
        "@types/ms" in config["compilerOptions"]["paths"],
        "and the @types package keeps its own: " + str(_keys_for(config, "@types/ms")),
    )
    return analysistest.end(env)

paths_omit_alias_test = analysistest.make(_paths_omit_alias_impl)

def _paths_redirect_impl(ctx):
    env = analysistest.begin(ctx)
    config = _compile_config(env)
    asserts.true(env, config != None, "ts_compile wrote no tsconfig")
    if config == None:
        return analysistest.end(env)

    # `ms` ships no declarations, so its key is normally redirected into
    # @types/ms. With that package excluded the redirection has to stop: a key
    # still pointing there would reach the declarations under the one name the
    # entry did not exclude.
    asserts.equals(
        env,
        [],
        [entry for entry in config["compilerOptions"]["paths"][_ALIAS] if "types_ms" in entry],
        "no key resolves into the excluded package: " + str(config["compilerOptions"]["paths"][_ALIAS]),
    )

    # The runtime package is not what left, so its own name still answers.
    asserts.true(
        env,
        len([entry for entry in config["compilerOptions"]["paths"][_ALIAS] if "npm_features__ms__" in entry]) == 1,
        "and it resolves to the runtime package: " + str(config["compilerOptions"]["paths"][_ALIAS]),
    )
    asserts.equals(
        env,
        [],
        [entry for entry in config.get("files", []) if "types_ms" in entry],
        "and its declarations reach `files` from nowhere",
    )
    return analysistest.end(env)

paths_redirect_test = analysistest.make(_paths_redirect_impl)

def _editor_keeps_ambient_file_impl(ctx):
    env = analysistest.begin(ctx)
    config = _editor_config(env)
    asserts.true(env, config != None, "ide_tsconfig wrote no tsconfig")
    if config == None:
        return analysistest.end(env)

    # `files` is the route a types-only dep takes with no import naming it, and
    # the editor has its own walk of it. Without the attribute the entry is here.
    asserts.true(
        env,
        len(_ambient_entries(config)) == 1,
        "the editor names its entry point in `files`: " + str(config.get("files")),
    )
    return analysistest.end(env)

editor_keeps_ambient_file_test = analysistest.make(_editor_keeps_ambient_file_impl)

def _editor_omits_ambient_file_impl(ctx):
    env = analysistest.begin(ctx)
    config = _editor_config(env)
    asserts.true(env, config != None, "ide_tsconfig wrote no tsconfig")
    if config == None:
        return analysistest.end(env)

    asserts.equals(
        env,
        [],
        _ambient_entries(config),
        "the editor leaves it out of `files` too: " + str(config.get("files")),
    )
    asserts.equals(
        env,
        [],
        _keys_for(config, _PACKAGE),
        "and resolves it from no key",
    )
    asserts.equals(
        env,
        [],
        [dest for dest in _installed(env) if "workers-types" in dest],
        "and installs nothing under npm_dir",
    )
    return analysistest.end(env)

editor_omits_ambient_file_test = analysistest.make(_editor_omits_ambient_file_impl)

def _dep_keeps_impl(ctx):
    env = analysistest.begin(ctx)
    config = _compile_config(env)
    asserts.true(env, config != None, "ts_compile wrote no tsconfig")
    if config == None:
        return analysistest.end(env)

    # This target depends on :shimmed, which keeps wrangler out of ITS program.
    # An attribute that travelled through the dep edge would take both keys away
    # here, and sibling.ts would stop compiling -- it names `Fetcher`, which the
    # global script behind wrangler is the only declaration of.
    asserts.true(
        env,
        "wrangler" in config["compilerOptions"]["paths"],
        "the dep's exclusion did not travel: " + str(_keys_for(config, "wrangler")),
    )
    asserts.equals(
        env,
        [_PACKAGE, _PACKAGE + "/*"],
        _keys_for(config, _PACKAGE),
        "and neither did anything behind it",
    )
    return analysistest.end(env)

dep_keeps_untyped_test = analysistest.make(_dep_keeps_impl)

def _editor_omits_impl(ctx):
    env = analysistest.begin(ctx)
    config = _editor_config(env)
    asserts.true(env, config != None, "ide_tsconfig wrote no tsconfig")
    if config == None:
        return analysistest.end(env)

    asserts.equals(
        env,
        [],
        _keys_for(config, _PACKAGE),
        "the editor resolves it from no key either",
    )
    asserts.true(
        env,
        "wrangler" in config["compilerOptions"]["paths"],
        "and still resolves the dep it arrived through",
    )

    # The declarations themselves: a `paths` key is only half the editor's
    # answer, since the tsserver hook resolves through the installed copy.
    asserts.equals(
        env,
        [],
        [dest for dest in _installed(env) if "workers-types" in dest],
        "nothing installs them under npm_dir",
    )
    return analysistest.end(env)

editor_omits_untyped_test = analysistest.make(_editor_omits_impl)

def _editor_keeps_impl(ctx):
    env = analysistest.begin(ctx)
    config = _editor_config(env)
    asserts.true(env, config != None, "ide_tsconfig wrote no tsconfig")
    if config == None:
        return analysistest.end(env)

    asserts.true(
        env,
        _PACKAGE in config["compilerOptions"]["paths"],
        "the same graph without the attribute resolves it: " + str(_keys_for(config, _PACKAGE)),
    )
    asserts.true(
        env,
        len([dest for dest in _installed(env) if "workers-types" in dest]) > 0,
        "and installs the declarations under npm_dir",
    )
    return analysistest.end(env)

editor_keeps_untyped_test = analysistest.make(_editor_keeps_impl)

def _editor_host_only_impl(ctx):
    env = analysistest.begin(ctx)
    config = _editor_config(env)
    asserts.true(env, config != None, "ide_tsconfig wrote no tsconfig")
    if config == None:
        return analysistest.end(env)

    # The same two targets that fail without it. host_only_packages is the
    # workspace-wide answer, so :sibling loses the package in the editor too --
    # which is the trade the failure names rather than makes silently.
    asserts.equals(
        env,
        [],
        _keys_for(config, _PACKAGE),
        "host_only_packages drops it for every program here",
    )
    return analysistest.end(env)

editor_host_only_test = analysistest.make(_editor_host_only_impl)

def _fails_with(message):
    def _impl(ctx):
        env = analysistest.begin(ctx)
        asserts.expect_failure(env, message)
        return analysistest.end(env)

    return analysistest.make(_impl, expect_failure = True)

editor_disagreement_test = _fails_with("keeps \"@cloudflare/workers-types\" out of its type program")
unmatched_name_test = _fails_with("which no dep of\n  this target resolves")
types_conflict_test = _fails_with("in both compilerOptions.types and")
