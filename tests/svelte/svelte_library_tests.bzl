"""Analysis-time coverage for svelte_library.

The compiler itself lives in an npm package, so what it produces is asserted by
//tests/integration:svelte_test against a real svelte install. What is checked
here is everything visible without running it: the three declared outputs per
source, which provider each one leaves by, and the two arguments a wrong value
would make the output non-reproducible or the wrong dialect.
"""

load("@bazel_skylib//lib:unittest.bzl", "analysistest", "asserts")
load("//ts/private:providers.bzl", "CssInfo", "JsInfo")

_PKG = "tests/svelte"

def _relative(files):
    marker = _PKG + "/"
    return sorted([f.path[f.path.find(marker) + len(marker):] for f in files])

def _compile_argv(env, source_basename):
    for action in analysistest.target_actions(env):
        if action.mnemonic != "SvelteCompile":
            continue
        for output in action.outputs.to_list():
            if output.basename == source_basename + ".js":
                return action.argv
    return None

def _flag(argv, name):
    for i in range(len(argv) - 1):
        if argv[i] == name:
            return argv[i + 1]
    return None

def _declared_outputs_impl(ctx):
    env = analysistest.begin(ctx)
    target = analysistest.target_under_test(env)
    asserts.equals(
        env,
        [
            "Card.svelte.css",
            "Card.svelte.js",
            "Card.svelte.js.map",
            "nested/Plain.svelte.css",
            "nested/Plain.svelte.js",
            "nested/Plain.svelte.js.map",
        ],
        _relative(target[DefaultInfo].files.to_list()),
        "declared outputs",
    )
    return analysistest.end(env)

declared_outputs_test = analysistest.make(_declared_outputs_impl)

def _providers_impl(ctx):
    env = analysistest.begin(ctx)
    target = analysistest.target_under_test(env)

    asserts.equals(
        env,
        ["Card.svelte.js", "nested/Plain.svelte.js"],
        _relative(target[JsInfo].js_files.to_list()),
        "JsInfo.js_files",
    )
    asserts.equals(
        env,
        ["Card.svelte.js.map", "nested/Plain.svelte.js.map"],
        _relative(target[JsInfo].js_map_files.to_list()),
        "JsInfo.js_map_files",
    )
    asserts.equals(
        env,
        ["Card.svelte.css", "nested/Plain.svelte.css"],
        _relative(target[CssInfo].css_files.to_list()),
        "CssInfo.css_files",
    )
    return analysistest.end(env)

providers_test = analysistest.make(_providers_impl)

def _forwarded_deps_impl(ctx):
    env = analysistest.begin(ctx)
    target = analysistest.target_under_test(env)

    # The CSS a dep contributes is reachable transitively and absent from the
    # direct set, which is what tells a consumer which target owns a file.
    transitive_css = _relative(target[CssInfo].transitive_css_files.to_list())
    asserts.true(
        env,
        "theme.css" in transitive_css,
        "the dep's CSS is not in transitive_css_files: " + str(transitive_css),
    )
    asserts.equals(
        env,
        ["Themed.svelte.css"],
        _relative(target[CssInfo].css_files.to_list()),
        "css_files carries only what this target compiled",
    )
    return analysistest.end(env)

forwarded_deps_test = analysistest.make(_forwarded_deps_impl)

def _client_argv_impl(ctx):
    env = analysistest.begin(ctx)
    argv = _compile_argv(env, "Card.svelte")
    asserts.true(env, argv != None, "no SvelteCompile action for Card.svelte")
    if argv == None:
        return analysistest.end(env)

    asserts.equals(env, "client", _flag(argv, "--generate"), "--generate")
    asserts.equals(env, "false", _flag(argv, "--dev"), "--dev")

    # The compiler hashes this name into the scope class it writes into both
    # outputs, so a configuration-dependent path makes every build a cache miss.
    asserts.equals(
        env,
        _PKG + "/Card.svelte",
        _flag(argv, "--filename"),
        "--filename is the workspace-relative path",
    )
    return analysistest.end(env)

client_argv_test = analysistest.make(_client_argv_impl)

def _server_argv_impl(ctx):
    env = analysistest.begin(ctx)
    argv = _compile_argv(env, "Badge.svelte")
    asserts.true(env, argv != None, "no SvelteCompile action for Badge.svelte")
    if argv == None:
        return analysistest.end(env)
    asserts.equals(env, "server", _flag(argv, "--generate"), "--generate")
    asserts.equals(env, "true", _flag(argv, "--dev"), "--dev")
    return analysistest.end(env)

server_argv_test = analysistest.make(_server_argv_impl)
