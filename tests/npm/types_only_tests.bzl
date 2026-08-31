"""Which packages ship ambient declarations rather than a module to import.

`is_types_package` is decided from the name -- everything under `@types/` and
nothing else -- but a package can be types-only under any name. For one that is,
the globals it exists to declare reach nothing: `types` cannot name it, and
importing from it fails because ambient declarations export nothing.
"""

load("@bazel_skylib//lib:unittest.bzl", "asserts", "unittest")
load("//ts/private:ts_npm_package.bzl", "declares_only_types")

_CASES = [
    struct(
        shape = "@cloudflare/workers-types: declarations under any name but @types/",
        files = ["package.json", "index.d.ts", "index.ts.md"],
        expected = True,
    ),
    struct(
        shape = "a normal dependency that also ships its declarations",
        files = ["package.json", "index.js", "index.d.ts"],
        expected = False,
    ),
    struct(
        shape = "ESM and CJS builds beside declarations",
        files = ["package.json", "dist/index.mjs", "dist/index.cjs", "dist/index.d.ts"],
        expected = False,
    ),
    struct(
        shape = "a package with no declarations at all",
        files = ["package.json", "index.js"],
        expected = False,
    ),
    struct(
        shape = "metadata only, nothing to declare",
        files = ["package.json", "README.md"],
        expected = False,
    ),
    struct(
        shape = "declarations in the .d.mts spelling",
        files = ["package.json", "index.d.mts"],
        expected = True,
    ),
]

def _declares_only_types_test(ctx):
    env = unittest.begin(ctx)
    for case in _CASES:
        asserts.equals(
            env,
            case.expected,
            declares_only_types(case.files),
            case.shape,
        )
    return unittest.end(env)

declares_only_types_test = unittest.make(_declares_only_types_test)

def types_only_test_suite(name):
    unittest.suite(name, declares_only_types_test)
