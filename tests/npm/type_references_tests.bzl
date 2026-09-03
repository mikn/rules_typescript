"""What a declaration's triple-slash header names, read where the file is.

TypeScript resolves `/// <reference types="x" />` through typeRoots and a
node_modules walk from the referencing file, never through `paths`; a consumer's
sandbox has neither, so the directive resolves to nothing and skipLibCheck hides
the TS2688. npm_import reads the header instead and writes the names into the
package's stanza. The bun-types header below is the published one, abridged:
all but two of its `/// <reference path=` lines are cut.
"""

load("@bazel_skylib//lib:unittest.bzl", "asserts", "unittest")
load("//npm/private:npm_import.bzl", "package_stanza", "referenced_types", "triple_slash_directives", "type_references")

_BUN_TYPES_HEADER = """// Project: https://github.com/oven-sh/bun
// Definitions by: Bun Contributors <https://github.com/oven-sh/bun/graphs/contributors>
// Definitions: https://github.com/DefinitelyTyped/DefinitelyTyped

/// <reference types="node" />

/// <reference path="./globals.d.ts" />
/// <reference path="./s3.d.ts" />

// Must disable this so it doesn't conflict with the DOM onmessage type, but still
// allows us to declare our own globals that Node's types can "see" and not conflict with
declare var onmessage: Bun.__internal.UseLibDomIfAvailable<"onmessage", never>;
"""

_DIRECTIVE_CASES = [
    struct(
        shape = "@types/bun: the whole file is one directive",
        content = '/// <reference types="bun-types" />\n',
        expected = [("types", "bun-types")],
    ),
    struct(
        shape = "bun-types: line comments and blank lines above, a statement below",
        content = _BUN_TYPES_HEADER,
        expected = [("types", "node"), ("path", "./globals.d.ts"), ("path", "./s3.d.ts")],
    ),
    struct(
        shape = "a directive below the first statement is a comment",
        content = 'declare var x: number;\n/// <reference types="node" />\n',
        expected = [],
    ),
    struct(
        shape = "single quotes, with resolution-mode beside the name",
        content = "/// <reference types='node' resolution-mode=\"require\" />\n",
        expected = [("types", "node")],
    ),
    struct(
        shape = "lib and no-default-lib name nothing a dep supplies",
        content = '/// <reference no-default-lib="true"/>\n/// <reference lib="es2015" />\n',
        expected = [],
    ),
    struct(
        shape = "a licence block above the directive",
        content = '/**\n * Copyright\n */\n/// <reference types="node" />\n',
        expected = [("types", "node")],
    ),
    struct(
        shape = "a block comment closed on a line that goes on to a statement",
        content = '/* header */ declare var x: number;\n/// <reference types="node" />\n',
        expected = [],
    ),
    struct(
        shape = "four slashes is a comment, whatever it says",
        content = '//// <reference types="node" />\n',
        expected = [],
    ),
]

def _directives_test(ctx):
    env = unittest.begin(ctx)
    for case in _DIRECTIVE_CASES:
        asserts.equals(env, case.expected, triple_slash_directives(case.content), case.shape)
    return unittest.end(env)

directives_test = unittest.make(_directives_test)

def _reader(files):
    def read(path):
        return files.get(path)

    return read

_REFERENCE_CASES = [
    struct(
        shape = "@types/bun forwards to bun-types",
        files = {"index.d.ts": '/// <reference types="bun-types" />\n'},
        entry = "index.d.ts",
        expected = ["bun-types"],
    ),
    struct(
        shape = "a sibling pulled in by path carries a directive of its own",
        files = {
            "index.d.ts": '/// <reference types="node" />\n/// <reference path="./globals.d.ts" />\n',
            "globals.d.ts": '/// <reference types="undici-types" />\ndeclare var g: number;\n',
        },
        entry = "index.d.ts",
        expected = ["node", "undici-types"],
    ),
    struct(
        shape = "path directives that cycle terminate",
        files = {
            "a.d.ts": '/// <reference path="./b.d.ts" />\n',
            "b.d.ts": '/// <reference path="./a.d.ts" />\n/// <reference types="x" />\n',
        },
        entry = "a.d.ts",
        expected = ["x"],
    ),
    struct(
        shape = "a path the package does not ship is skipped, not failed",
        files = {"index.d.ts": '/// <reference path="./missing.d.ts" />\n'},
        entry = "index.d.ts",
        expected = [],
    ),
    struct(
        shape = "a path climbing out of the package is skipped",
        files = {
            "index.d.ts": '/// <reference path="../outside.d.ts" />\n',
            "outside.d.ts": '/// <reference types="x" />\n',
        },
        entry = "index.d.ts",
        expected = [],
    ),
    struct(
        shape = "a path resolves against the referencing file's directory",
        files = {
            "dist/index.d.ts": '/// <reference path="./sub/x.d.ts" />\n',
            "dist/sub/x.d.ts": '/// <reference types="y" />\n',
            "sub/x.d.ts": '/// <reference types="wrong" />\n',
        },
        entry = "dist/index.d.ts",
        expected = ["y"],
    ),
    struct(
        shape = "a header with no directive",
        files = {"index.d.ts": "export declare const x: number;\n"},
        entry = "index.d.ts",
        expected = [],
    ),
]

def _referenced_types_test(ctx):
    env = unittest.begin(ctx)
    for case in _REFERENCE_CASES:
        asserts.equals(env, case.expected, referenced_types(_reader(case.files), case.entry), case.shape)
    return unittest.end(env)

referenced_types_test = unittest.make(_referenced_types_test)

def _written_form_test(ctx):
    env = unittest.begin(ctx)
    files = {
        "index.d.ts": '/// <reference types="bun-types" />\n',
        "client.d.ts": "export declare const x: number;\n",
    }
    references = type_references(_reader(files), ["index.d.ts", "client.d.ts"])
    asserts.equals(
        env,
        {"index.d.ts": ["bun-types"]},
        references,
        "only a declaration that names something gets an entry",
    )
    stanza = package_stanza(
        struct(version = "1.3.5", peer_id = "", types_dep = "", is_types_package = True),
        "pkg",
        "@types/bun",
        "",
        "index.d.ts",
        {},
        references,
    )
    asserts.true(
        env,
        '    type_references = {\n        "index.d.ts": ["bun-types"],\n    },\n' in stanza,
        "the stanza carries the attribute as written: " + stanza,
    )
    asserts.true(
        env,
        "type_references" not in package_stanza(
            struct(version = "1.3.5", peer_id = "", types_dep = "", is_types_package = True),
            "pkg",
            "@types/bun",
            "",
            "index.d.ts",
            {},
            {},
        ),
        "and a package that references nothing does not name the attribute",
    )
    return unittest.end(env)

written_form_test = unittest.make(_written_form_test)

def type_references_test_suite(name):
    unittest.suite(name, directives_test, referenced_types_test, written_form_test)
