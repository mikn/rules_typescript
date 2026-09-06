"""A workspace member's package.json as the compiled package's.

The hub's view of a member links the member's own manifest into node_modules,
with every source-file target rewritten to the file ts_compile emits from it:
`main`, `module`, `browser`, `exports` and `imports` name the `.js`, `types`,
`typings` and a `types` condition under `exports` name the `.d.ts`. tsc maps a
`.js` target to the `.d.ts` beside it and node runs the `.js`, so one manifest
serves the check and the runtime.

The text keeps the manifest's key order. An `exports` condition map is read in
the order it is written, and Bazel's json.encode sorts keys -- `default` would
come ahead of `types` -- so the rewrite encodes the manifest itself.
"""

load("@bazel_skylib//lib:unittest.bzl", "asserts", "unittest")
load("//npm/private:member_manifest.bzl", "member_manifest_json")

_CASES = [
    struct(
        shape = "exports subpaths, the @lovable/canvas-sdk shape: .ts targets become .js, " +
                "a css target and a null stay, and fields no resolver reads are untouched",
        manifest = {
            "name": "@lovable/canvas-sdk",
            "private": True,
            "type": "module",
            "exports": {
                "./wire": "./src/wire/index.ts",
                "./client/styles.css": "./src/client/styles.css",
                "./internal/*": None,
            },
            "files": ["src/*"],
            "scripts": {"typecheck": "tsc --noEmit"},
        },
        expected = '{"name":"@lovable/canvas-sdk","private":true,"type":"module",' +
                   '"exports":{"./wire":"./src/wire/index.js","./client/styles.css":"./src/client/styles.css","./internal/*":null},' +
                   '"files":["src/*"],"scripts":{"typecheck":"tsc --noEmit"}}',
    ),
    struct(
        shape = "conditions in the order the map writes them: types names the .d.ts, default the .js",
        manifest = {"name": "pulse", "exports": {".": {"types": "./entry.ts", "default": "./dist/entry.js"}}},
        expected = '{"name":"pulse","exports":{".":{"types":"./entry.d.ts","default":"./dist/entry.js"}},"type":"module"}',
    ),
    struct(
        shape = "the same conditions written the other way round keep that order",
        manifest = {"name": "pulse", "exports": {".": {"default": "./entry.ts", "types": "./entry.ts"}}},
        expected = '{"name":"pulse","exports":{".":{"default":"./entry.js","types":"./entry.d.ts"}},"type":"module"}',
    ),
    struct(
        shape = "a fallback array and a wildcard target",
        manifest = {"name": "m", "exports": {".": ["./first.ts", "./second.js"], "./tokens/*": "./styles/tokens/*.ts"}},
        expected = '{"name":"m","exports":{".":["./first.js","./second.js"],"./tokens/*":"./styles/tokens/*.js"},"type":"module"}',
    ),
    struct(
        shape = "exports as a bare string",
        manifest = {"name": "ws-linked", "exports": "./index.ts"},
        expected = '{"name":"ws-linked","exports":"./index.js","type":"module"}',
    ),
    struct(
        shape = "main, module and browser name the .js; types and typings the .d.ts",
        manifest = {
            "name": "m",
            "main": "./src/index.ts",
            "module": "src/index.ts",
            "browser": "./src/browser.tsx",
            "types": "./src/index.ts",
            "typings": "src/legacy.tsx",
        },
        expected = '{"name":"m","main":"./src/index.js","module":"src/index.js","browser":"./src/browser.js",' +
                   '"types":"./src/index.d.ts","typings":"src/legacy.d.ts","type":"module"}',
    ),
    struct(
        shape = "imports, the package's own #-specifiers",
        manifest = {"name": "m", "imports": {"#internal/*": "./src/internal/*.ts", "#dep": "zod"}},
        expected = '{"name":"m","imports":{"#internal/*":"./src/internal/*.js","#dep":"zod"},"type":"module"}',
    ),
    struct(
        shape = ".mts and .cts keep their module format in both roles",
        manifest = {"name": "m", "main": "./a.cts", "module": "./b.mts", "types": "./a.cts", "exports": {".": {"types": "./b.mts", "import": "./b.mts"}}},
        expected = '{"name":"m","main":"./a.cjs","module":"./b.mjs","types":"./a.d.cts",' +
                   '"exports":{".":{"types":"./b.d.mts","import":"./b.mjs"}},"type":"module"}',
    ),
    struct(
        shape = "a declaration target is already the emitted file",
        manifest = {"name": "m", "types": "./dist/index.d.ts", "exports": {".": {"types": "./dist/index.d.mts", "default": "./dist/index.d.cts"}}},
        expected = '{"name":"m","types":"./dist/index.d.ts","exports":{".":{"types":"./dist/index.d.mts","default":"./dist/index.d.cts"}},"type":"module"}',
    ),
    struct(
        shape = "type is kept when the member sets it",
        manifest = {"name": "m", "type": "commonjs", "main": "./index.ts"},
        expected = '{"name":"m","type":"commonjs","main":"./index.js"}',
    ),
    struct(
        shape = "scalars survive the round trip",
        manifest = {"name": "m", "version": "0.0.0", "sideEffects": False, "engines": {"node": 22}},
        expected = '{"name":"m","version":"0.0.0","sideEffects":false,"engines":{"node":22},"type":"module"}',
    ),
    struct(
        shape = "no name: not a member, and nothing to carry",
        manifest = {"version": "0.0.0", "main": "./index.ts"},
        expected = None,
    ),
    struct(
        shape = "not a manifest at all",
        manifest = ["./index.ts"],
        expected = None,
    ),
]

def _member_manifest_json_test(ctx):
    env = unittest.begin(ctx)
    for case in _CASES:
        asserts.equals(env, case.expected, member_manifest_json(case.manifest), case.shape)
    return unittest.end(env)

member_manifest_json_test = unittest.make(_member_manifest_json_test)

def member_manifest_test_suite(name):
    unittest.suite(name, member_manifest_json_test)
