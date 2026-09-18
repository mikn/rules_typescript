"""Unit tests for the go.mod and go.sum readers behind the source-built tsgo.

The readers are text -> struct with no fetch in them, so every message the ts
extension can raise from ts/private/tsgo_source is pinned here; building
//ts/toolchain/tsgo_source:tsgo_source_impl pins the fetch and the build the
structs drive.
"""

load("@bazel_skylib//lib:unittest.bzl", "asserts", "unittest")
load(
    "//ts/private:go_mod.bzl",
    "go_repo_name",
    "parse_go_mod",
    "parse_go_sum",
    "tsgo_source_modules",
)

_TSGO = "github.com/microsoft/typescript-go"
_TSGO_VERSION = "v0.0.0-20260708042240-2bd066d87f5b"
_TSGO_SUM = "h1:IgfPOv9pzPZ1sbyRHOKu3UCvhaTS0IyrkTcs6JLPqhY="
_TSGO_GO_MOD_SUM = "h1:gV4G6M8xxLNqQSRHiFOgKHlY+5YJgYPM6QwFLWllC+8="
_SYS = "golang.org/x/sys"
_JSON = "github.com/go-json-experiment/json"
_SYS_VERSION = "v0.46.0"
_SYS_SUM = "h1:noSf2Fq6F8DBgS+LysIkx7rIExoNHJsxOAtPp4rthXw="
_SYS_GO_MOD_SUM = "h1:4GL1E5IUh+htKOUEOaiffhrAeqysfVGipDYzABqnCmw="

_TOOL_LINE = "tool " + _TSGO + "/cmd/tsgo\n"
_TSGO_REQUIRE = "\t" + _TSGO + " " + _TSGO_VERSION + " // indirect\n"

# ts/private/tsgo_source/go.mod as `go mod tidy` writes it, cut to two modules.
_GO_MOD = (
    "module github.com/mikn/rules_typescript/ts/private/tsgo_source\n\n" +
    "go 1.26.1\n\n" +
    _TOOL_LINE + "\n" +
    "require (\n" +
    _TSGO_REQUIRE +
    "\t" + _SYS + " " + _SYS_VERSION + " // indirect\n" +
    ")\n"
)

def _sum_lines(path, version, sum, go_mod_sum):
    return "{p} {v} {s}\n{p} {v}/go.mod {m}\n".format(
        p = path,
        v = version,
        s = sum,
        m = go_mod_sum,
    )

_GO_SUM = (
    _sum_lines(_TSGO, _TSGO_VERSION, _TSGO_SUM, _TSGO_GO_MOD_SUM) +
    _sum_lines(_SYS, _SYS_VERSION, _SYS_SUM, _SYS_GO_MOD_SUM)
)

_GO_MOD_NAME = "//ts/private/tsgo_source:go.mod"

def _parse_test(ctx):
    env = unittest.begin(ctx)

    parsed = parse_go_mod(_GO_MOD)
    asserts.equals(env, [_TSGO + "/cmd/tsgo"], parsed.tools)
    asserts.equals(
        env,
        {_TSGO: _TSGO_VERSION, _SYS: _SYS_VERSION},
        parsed.requires,
    )

    single = parse_go_mod(
        "module m\n\ngo 1.26.1\n\ntool a.example/b/cmd/c\n\n" +
        "require a.example/b v1.2.3 // indirect\n",
    )
    asserts.equals(env, ["a.example/b/cmd/c"], single.tools)
    asserts.equals(env, {"a.example/b": "v1.2.3"}, single.requires)

    asserts.equals(
        env,
        {
            (_TSGO, _TSGO_VERSION): _TSGO_SUM,
            (_SYS, _SYS_VERSION): _SYS_SUM,
        },
        parse_go_sum(_GO_SUM),
    )

    return unittest.end(env)

parse_test = unittest.make(_parse_test)

def _repo_name_test(ctx):
    env = unittest.begin(ctx)

    for importpath, name in {
        _TSGO: "com_github_microsoft_typescript_go",
        "github.com/Microsoft/go-winio": "com_github_microsoft_go_winio",
        "github.com/klauspost/cpuid/v2": "com_github_klauspost_cpuid_v2",
        _JSON: "com_github_go_json_experiment_json",
        _SYS: "org_golang_x_sys",
    }.items():
        asserts.equals(env, name, go_repo_name(importpath))

    return unittest.end(env)

repo_name_test = unittest.make(_repo_name_test)

def _modules_test(ctx):
    env = unittest.begin(ctx)

    source = tsgo_source_modules(_GO_MOD, _GO_SUM, _GO_MOD_NAME)
    asserts.equals(env, "", source.error)
    asserts.equals(env, _TSGO + "/cmd/tsgo", source.tool.package)
    asserts.equals(env, _TSGO, source.tool.module)
    asserts.equals(env, _TSGO_VERSION, source.tool.version)
    asserts.equals(
        env,
        [
            struct(path = _TSGO, version = _TSGO_VERSION, sum = _TSGO_SUM),
            struct(path = _SYS, version = _SYS_VERSION, sum = _SYS_SUM),
        ],
        source.modules,
    )

    return unittest.end(env)

modules_test = unittest.make(_modules_test)

def _errors_test(ctx):
    env = unittest.begin(ctx)

    no_tool = tsgo_source_modules(
        _GO_MOD.replace(_TOOL_LINE, ""),
        _GO_SUM,
        _GO_MOD_NAME,
    )
    asserts.true(
        env,
        no_tool.error.startswith(_GO_MOD_NAME + " names 0 tool directives;"),
        no_tool.error,
    )

    two_tools = tsgo_source_modules(
        _GO_MOD + "\ntool " + _SYS + "/cmd/other\n",
        _GO_SUM,
        _GO_MOD_NAME,
    )
    asserts.true(
        env,
        two_tools.error.startswith(_GO_MOD_NAME + " names 2 tool directives;"),
        two_tools.error,
    )

    unrequired = tsgo_source_modules(
        _GO_MOD.replace(_TSGO_REQUIRE, ""),
        _GO_SUM,
        _GO_MOD_NAME,
    )
    asserts.equals(
        env,
        _GO_MOD_NAME + " requires no module holding its tool " + _TSGO +
        "/cmd/tsgo; run `go mod tidy` in its directory.",
        unrequired.error,
    )

    unsummed = tsgo_source_modules(_GO_MOD, "", _GO_MOD_NAME)
    asserts.equals(
        env,
        "go.sum beside " + _GO_MOD_NAME + " has no h1 sum for " + _TSGO + " " +
        _TSGO_VERSION + "; run `go mod tidy` in its directory.",
        unsummed.error,
    )

    return unittest.end(env)

errors_test = unittest.make(_errors_test)

def go_mod_test_suite(name):
    unittest.suite(
        name,
        parse_test,
        repo_name_test,
        modules_test,
        errors_test,
    )
