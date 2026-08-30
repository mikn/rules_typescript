"""Which declaration a package's own metadata designates, and that it reaches tsgo.

Every manifest below is the published `package.json` of a package in one of this
repo's lockfiles -- formdata-node, which is in a consumer's, is the exception --
quoted rather than composed, and `files` is the declarations that tarball
actually ships. Invented fixtures cannot pin this: the shapes that break a
resolver are the ones npm publishers reach for -- a bare string entry, a `types`
condition two levels down, an array fallback, a conditions map with no subpaths
at all -- and a hand-written table would only ever contain the shapes already
handled.

What the rows pin, beyond one expected path each:

  ORDER. The first matching condition in the map's OWN key order wins, so
  consola answers with its `node` build and not the browser build under
  `default`, and tinybench -- which writes `require` before `import` -- answers
  with the CommonJS declarations a fixed condition priority would have walked
  past. Both packages ship declarations for every branch, so the wrong order
  still produces a real .d.ts of a real build.

  FALLBACK. `exports` is authoritative about what it designates and silent about
  the rest; `types`/`typings` is where the silent majority of npm publishes; and
  a package that designates nothing at all may still ship index.d.ts, which is
  where TypeScript's own resolution ends. A package that resolves to nothing is
  a package whose typecheck runs against no declarations at all, which no error
  mentions.

  EXISTENCE. A manifest may name a declaration the tarball omits -- six packages
  in this closure do. Taking the name on trust generates a target whose source
  does not exist, which fails only once some action wants the file.

  FORM. The answer is written into a label-valued attribute, so a path whose
  first segment starts with `@` -- formdata-node keeps its declarations in a
  directory called `@type` -- is read as a repository name unless it is written
  as a path, and Bazel rejects the entire generated repository over it.
"""

load("@bazel_skylib//lib:unittest.bzl", "analysistest", "asserts", "unittest")
load("//npm/private:npm_import.bzl", "exports_types", "package_stanza")
load("//ts/private:providers.bzl", "NpmPackageInfo")

_CASES = [
    struct(
        package = "vite@8.2.2",
        shape = "exports['.'] is a bare string and there is no `types` field",
        manifest = """{
          "name": "vite",
          "version": "8.2.2",
          "exports": {
            ".": "./dist/node/index.js",
            "./client": { "types": "./client.d.ts" },
            "./module-runner": "./dist/node/module-runner.js",
            "./types/*": { "types": "./types/*" },
            "./types/internal/*": null,
            "./package.json": "./package.json"
          }
        }""",
        files = ["client.d.ts", "dist/node/index.d.ts", "dist/node/module-runner.d.ts"],
        expected = "dist/node/index.d.ts",
    ),
    struct(
        package = "vite@6.4.1",
        shape = "conditions with no `types` among them; the CommonJS branch also ships one",
        manifest = """{
          "name": "vite",
          "version": "6.4.1",
          "main": "./dist/node/index.js",
          "types": "./dist/node/index.d.ts",
          "exports": {
            ".": {
              "module-sync": "./dist/node/index.js",
              "import": "./dist/node/index.js",
              "require": "./index.cjs"
            },
            "./client": { "types": "./client.d.ts" },
            "./package.json": "./package.json"
          }
        }""",
        files = ["client.d.ts", "dist/node/index.d.ts", "index.d.cts"],
        expected = "dist/node/index.d.ts",
    ),
    struct(
        package = "vitest@4.1.11",
        shape = "`types` nested under `import`, with a second one under `require`",
        manifest = """{
          "name": "vitest",
          "version": "4.1.11",
          "main": "./dist/index.js",
          "types": "./dist/index.d.ts",
          "exports": {
            ".": {
              "import": { "types": "./dist/index.d.ts", "default": "./dist/index.js" },
              "require": { "types": "./index.d.cts", "default": "./index.cjs" }
            },
            "./globals": { "types": "./globals.d.ts" },
            "./package.json": "./package.json"
          }
        }""",
        files = ["dist/index.d.ts", "globals.d.ts", "index.d.cts"],
        expected = "dist/index.d.ts",
    ),
    struct(
        package = "minimatch@10.2.4",
        shape = "a subpath key before '.', and both branches ship declarations",
        manifest = """{
          "name": "minimatch",
          "version": "10.2.4",
          "main": "./dist/commonjs/index.js",
          "types": "./dist/commonjs/index.d.ts",
          "exports": {
            "./package.json": "./package.json",
            ".": {
              "import": { "types": "./dist/esm/index.d.ts", "default": "./dist/esm/index.js" },
              "require": { "types": "./dist/commonjs/index.d.ts", "default": "./dist/commonjs/index.js" }
            }
          }
        }""",
        files = ["dist/commonjs/index.d.ts", "dist/esm/index.d.ts"],
        expected = "dist/esm/index.d.ts",
    ),
    struct(
        package = "consola@3.4.2",
        shape = "`node` and `default` side by side, each with its own build's declarations",
        manifest = """{
          "name": "consola",
          "version": "3.4.2",
          "main": "./lib/index.cjs",
          "types": "./dist/index.d.ts",
          "exports": {
            ".": {
              "node": {
                "import": { "types": "./dist/index.d.mts", "default": "./dist/index.mjs" },
                "require": { "types": "./dist/index.d.cts", "default": "./lib/index.cjs" }
              },
              "default": {
                "import": { "types": "./dist/browser.d.mts", "default": "./dist/browser.mjs" },
                "require": { "types": "./dist/browser.d.cts", "default": "./dist/browser.cjs" }
              }
            },
            "./browser": {
              "import": { "types": "./dist/browser.d.mts", "default": "./dist/browser.mjs" },
              "require": { "types": "./dist/browser.d.cts", "default": "./dist/browser.cjs" }
            }
          }
        }""",
        files = [
            "dist/browser.d.cts",
            "dist/browser.d.mts",
            "dist/index.d.cts",
            "dist/index.d.mts",
            "dist/index.d.ts",
        ],
        expected = "dist/index.d.mts",
    ),
    struct(
        package = "acorn@8.18.0",
        shape = "exports['.'] is an array of fallbacks",
        manifest = """{
          "name": "acorn",
          "version": "8.18.0",
          "main": "dist/acorn.js",
          "types": "dist/acorn.d.ts",
          "exports": {
            ".": [
              {
                "import": "./dist/acorn.mjs",
                "require": "./dist/acorn.js",
                "default": "./dist/acorn.js"
              },
              "./dist/acorn.js"
            ],
            "./package.json": "./package.json"
          }
        }""",
        files = ["dist/acorn.d.mts", "dist/acorn.d.ts"],
        expected = "dist/acorn.d.mts",
    ),
    struct(
        package = "escalade@3.2.0",
        shape = "an array whose first element nests `types` under each condition",
        manifest = """{
          "name": "escalade",
          "version": "3.2.0",
          "main": "dist/index.js",
          "types": "index.d.ts",
          "exports": {
            ".": [
              {
                "import": { "types": "./index.d.mts", "default": "./dist/index.mjs" },
                "require": { "types": "./index.d.ts", "default": "./dist/index.js" }
              },
              "./dist/index.js"
            ],
            "./sync": [
              {
                "import": { "types": "./sync/index.d.mts", "default": "./sync/index.mjs" },
                "require": { "types": "./sync/index.d.ts", "default": "./sync/index.js" }
              },
              "./sync/index.js"
            ]
          }
        }""",
        files = ["index.d.mts", "index.d.ts", "sync/index.d.mts", "sync/index.d.ts"],
        expected = "index.d.mts",
    ),
    struct(
        package = "mlly@1.8.2",
        shape = "conditions at the top level, no subpaths: the map IS the entry point",
        manifest = """{
          "name": "mlly",
          "version": "1.8.2",
          "main": "./dist/index.cjs",
          "types": "./dist/index.d.ts",
          "exports": {
            "types": "./dist/index.d.ts",
            "import": "./dist/index.mjs",
            "require": "./dist/index.cjs"
          }
        }""",
        files = ["dist/index.d.cts", "dist/index.d.mts", "dist/index.d.ts"],
        expected = "dist/index.d.ts",
    ),
    struct(
        package = "tinybench@2.9.0",
        shape = "the same shorthand, no `types` condition, and `require` written first",
        manifest = """{
          "name": "tinybench",
          "version": "2.9.0",
          "main": "./dist/index.cjs",
          "types": "./dist/index.d.cts",
          "exports": {
            "require": "./dist/index.cjs",
            "import": "./dist/index.js",
            "default": "./dist/index.js"
          }
        }""",
        files = ["dist/index.d.cts", "dist/index.d.ts"],
        expected = "dist/index.d.cts",
    ),
    struct(
        package = "@babel/compat-data@7.29.0",
        shape = "subpaths only, no '.' -- not a conditions map, and it types nothing",
        manifest = """{
          "name": "@babel/compat-data",
          "version": "7.29.0",
          "exports": {
            "./plugins": "./plugins.js",
            "./native-modules": "./native-modules.js",
            "./package.json": "./package.json"
          }
        }""",
        files = [],
        expected = "",
    ),
    struct(
        package = "ansi-regex@6.2.2",
        shape = "`exports` is a bare string",
        manifest = """{
          "name": "ansi-regex",
          "version": "6.2.2",
          "types": "./index.d.ts",
          "exports": "./index.js"
        }""",
        files = ["index.d.ts"],
        expected = "index.d.ts",
    ),
    struct(
        package = "@types/node@22.20.1",
        shape = "no `exports` at all -- the majority of npm, and every @types package",
        manifest = """{
          "name": "@types/node",
          "version": "22.20.1",
          "main": "",
          "types": "index.d.ts"
        }""",
        files = ["assert.d.ts", "index.d.ts"],
        expected = "index.d.ts",
    ),
    struct(
        package = "ts-interface-checker@0.1.13",
        shape = "`typings`, extensionless, the way node10 resolution reads it",
        manifest = """{
          "name": "ts-interface-checker",
          "version": "0.1.13",
          "main": "dist/index",
          "typings": "dist/index"
        }""",
        files = ["dist/index.d.ts", "dist/types.d.ts", "dist/util.d.ts"],
        expected = "dist/index.d.ts",
    ),
    struct(
        package = "@babel/helper-string-parser@7.29.7",
        shape = "a `types` condition naming a declaration the tarball does not ship",
        manifest = """{
          "name": "@babel/helper-string-parser",
          "version": "7.29.7",
          "main": "./lib/index.js",
          "exports": {
            ".": { "types": "./lib/index.d.ts", "default": "./lib/index.js" },
            "./package.json": "./package.json"
          }
        }""",
        files = [],
        expected = "",
    ),
    struct(
        package = "load-tsconfig@0.2.5",
        shape = "`types` naming one it does not ship either",
        manifest = """{
          "name": "load-tsconfig",
          "version": "0.2.5",
          "types": "./dist/index.d.ts",
          "exports": {
            ".": { "import": "./dist/index.js", "default": "./dist/index.cjs" }
          }
        }""",
        files = [],
        expected = "",
    ),
    struct(
        package = "@cloudflare/workers-types@4.20260420.1",
        shape = "designates nothing and ships index.d.ts, TypeScript's own last resort",
        manifest = """{
          "name": "@cloudflare/workers-types",
          "description": "TypeScript typings for Cloudflare Workers",
          "license": "MIT OR Apache-2.0",
          "version": "4.20260420.1"
        }""",
        files = ["index.d.ts", "2023-07-01/index.d.ts"],
        expected = "index.d.ts",
    ),
    struct(
        package = "formdata-node@4.4.1",
        shape = "declarations under a directory named `@type`",
        manifest = """{
          "name": "formdata-node",
          "version": "4.4.1",
          "main": "./lib/cjs/index.js",
          "module": "./lib/esm/browser.js",
          "types": "./@type/index.d.ts",
          "exports": {
            ".": {
              "node": {
                "types": "./@type/index.d.ts",
                "import": "./lib/esm/index.js",
                "require": "./lib/cjs/index.js"
              },
              "browser": {
                "types": "./@type/browser.d.ts",
                "import": "./lib/esm/browser.js",
                "require": "./lib/cjs/browser.js"
              },
              "default": "./lib/esm/index.js"
            },
            "./package.json": "./package.json",
            "./file-from-path": {
              "types": "./@type/fileFromPath.d.ts",
              "import": "./lib/esm/fileFromPath.js",
              "require": "./lib/cjs/fileFromPath.js"
            }
          }
        }""",
        files = ["@type/browser.d.ts", "@type/fileFromPath.d.ts", "@type/index.d.ts"],
        expected = "@type/index.d.ts",
    ),
    struct(
        package = "balanced-match@1.0.2",
        shape = "a package that ships no declarations and claims none",
        manifest = """{
          "name": "balanced-match",
          "version": "1.0.2",
          "main": "index.js"
        }""",
        files = [],
        expected = "",
    ),
]

def _shipping(files):
    def has_file(path):
        return path in files

    return has_file

def _published_shapes_test(ctx):
    env = unittest.begin(ctx)

    for case in _CASES:
        asserts.equals(
            env,
            case.expected,
            exports_types(json.decode(case.manifest), _shipping(case.files)),
            "{}: {}".format(case.package, case.shape),
        )

    return unittest.end(env)

published_shapes_test = unittest.make(_published_shapes_test)

# Resolved against this package because the one the BUILD file is generated into
# exists only inside a fetch; whether a string names a path or a repository is
# the same question in either.
_GENERATED_PACKAGE = Label("//tests/npm:BUILD.bazel")

def _written_attribute(stanza, attribute):
    prefix = '    {} = "'.format(attribute)
    for line in stanza.split("\n"):
        if line.startswith(prefix):
            return line[len(prefix):].removesuffix('",')
    return ""

def _written_form_test(ctx):
    env = unittest.begin(ctx)

    for case in _CASES:
        if not case.expected:
            continue
        package_name, _, version = case.package.rpartition("@")
        stanza = package_stanza(
            struct(version = version, peer_id = "", types_dep = "", is_types_package = False),
            "pkg",
            package_name,
            "",
            exports_types(json.decode(case.manifest), _shipping(case.files)),
            {},
        )
        asserts.equals(
            env,
            case.expected,
            _GENERATED_PACKAGE.relative(_written_attribute(stanza, "exports_types")).name,
            "{}: exports_types names a file in the package, not a repository".format(case.package),
        )

    return unittest.end(env)

written_form_test = unittest.make(_written_form_test)

def exports_types_test_suite(name):
    unittest.suite(name, published_shapes_test, written_form_test)

def _written_tsconfig(env):
    for action in analysistest.target_actions(env):
        outputs = action.outputs.to_list()
        if len(outputs) == 1 and outputs[0].basename.endswith(".tsconfig.json"):
            return json.decode(action.content)
    return None

def _declaration_entry_impl(ctx):
    env = analysistest.begin(ctx)
    config = _written_tsconfig(env)
    asserts.true(env, config != None, "the target under test generated no tsconfig")
    if config == None:
        return analysistest.end(env)

    npm = ctx.attr.npm_package[NpmPackageInfo]
    entry = npm.exports_types_file
    resolved = "{}@{} -> {}".format(
        npm.package_name,
        npm.package_version,
        entry.path if entry else "no declaration entry",
    )

    asserts.true(env, entry != None, "the package designates a declaration: " + resolved)
    if entry == None:
        return analysistest.end(env)

    # Under the package root rather than beside it: a paths entry pointing into
    # some other resolution of the same name type-checks against a version this
    # target never depends on.
    asserts.true(
        env,
        entry.path.startswith(npm.package_dir.dirname + "/"),
        "the declaration belongs to the resolution under test: " + resolved,
    )

    mapped = config["compilerOptions"].get("paths", {}).get(npm.package_name, [])
    asserts.equals(env, 1, len(mapped), "one paths entry for " + npm.package_name + ": " + str(mapped))
    if len(mapped) != 1:
        return analysistest.end(env)

    # A directory here is the failure that reads as success: tsgo resolves it by
    # re-reading the package's own manifest, and answers with nothing when the
    # manifest is a shape it disagrees with.
    asserts.true(
        env,
        mapped[0].endswith(entry.path),
        "tsgo is pointed at the declaration itself, not a directory: {} vs {}".format(mapped[0], resolved),
    )

    return analysistest.end(env)

declaration_entry_test = analysistest.make(
    _declaration_entry_impl,
    attrs = {
        "npm_package": attr.label(
            mandatory = True,
            providers = [NpmPackageInfo],
            doc = "The npm package whose declarations the target under test type-checks against.",
        ),
    },
)
