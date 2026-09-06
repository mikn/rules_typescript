"""Which files a package's own metadata designates, and that the entry reaches tsgo.

Two files per package, in the roles TypeScript resolves them in: the MODULE
ENTRY a bare import resolves to, and the DECLARATION a `compilerOptions.types`
entry or a `/// <reference types>` directive resolves to. For most of npm the
two are one file; `expected` below is the declaration and `module` the module
entry where the two differ.

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
  the rest; `types`/`typings` is where the silent majority of npm publishes;
  `main` is read after them, and a package that designates nothing at all may
  still ship a root index, which is where TypeScript's own resolution ends. A
  package that resolves to nothing is a package whose typecheck runs against no
  declarations at all, which no error mentions.

  ROLES. A bare import takes `.ts` and `.tsx` ahead of `.d.ts` -- for a `.js`
  target, an extensionless field, the root index -- and a `types` entry takes
  declarations alone. @cloudflare/workers-types ships index.ts, a module, beside
  index.d.ts, a global script, and the one file answering both roles is TS2306
  for the import or no globals for the entry.

  EXISTENCE. A manifest may name a declaration the tarball omits -- six packages
  in this closure do. Taking the name on trust generates a target whose source
  does not exist, which fails only once some action wants the file.

  FORM. The answer is written into a label-valued attribute, so a path whose
  first segment starts with `@` -- formdata-node keeps its declarations in a
  directory called `@type` -- is read as a repository name unless it is written
  as a path, and Bazel rejects the entire generated repository over it.
"""

load("@bazel_skylib//lib:unittest.bzl", "analysistest", "asserts", "unittest")
load("//npm/private:npm_import.bzl", "exports_subpath_patterns", "exports_subpath_types", "exports_types", "module_entry", "package_stanza")
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
            "./internal": "./dist/node/internal.js",
            "./dist/client/*": "./dist/client/*",
            "./types/*": { "types": "./types/*" },
            "./types/internal/*": null,
            "./package.json": "./package.json"
          }
        }""",
        files = ["client.d.ts", "dist/node/index.d.ts", "dist/node/internal.d.ts", "dist/node/module-runner.d.ts"],
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
        shape = "designates nothing and ships index.ts, a module, beside index.d.ts, a global script",
        manifest = """{
          "name": "@cloudflare/workers-types",
          "description": "TypeScript typings for Cloudflare Workers",
          "license": "MIT OR Apache-2.0",
          "version": "4.20260420.1"
        }""",
        files = ["index.d.ts", "index.ts", "2023-07-01/index.d.ts"],
        expected = "index.d.ts",
        module = "index.ts",
    ),
    struct(
        package = "@humanfs/types@0.15.0",
        shape = "`exports` and `types` both name a .ts: a module entry, and nothing for a `types` entry",
        manifest = """{
          "name": "@humanfs/types",
          "version": "0.15.0",
          "type": "module",
          "types": "src/hfs-types.ts",
          "exports": {
            ".": {
              "types": "./src/hfs-types.ts"
            },
            "./package.json": "./package.json"
          }
        }""",
        files = ["src/hfs-types.ts"],
        expected = "",
        module = "src/hfs-types.ts",
    ),
    struct(
        package = "postcss-value-parser@4.2.0",
        shape = "`main` alone: the declaration beside it, which no `types` field names",
        manifest = """{
          "name": "postcss-value-parser",
          "version": "4.2.0",
          "main": "lib/index.js"
        }""",
        files = ["lib/index.d.ts"],
        expected = "lib/index.d.ts",
    ),
    struct(
        package = "blake3-wasm@2.1.5",
        shape = "an extensionless `main`; `module` is a bundler field no resolution reads",
        manifest = """{
          "name": "blake3-wasm",
          "version": "2.1.5",
          "main": "./dist/index",
          "module": "./esm/index"
        }""",
        files = ["dist/index.d.ts", "esm/index.d.ts"],
        expected = "dist/index.d.ts",
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

# The subpaths a manifest designates, which is its `exports` map and nothing else:
# web-streams-polyfill's typesVersions rewrite, which tsc follows, is not a designation.
_SUBPATH_CASES = [
    struct(
        package = "vite@8.2.2",
        shape = "a `types` condition, two .js whose declarations sit beside them, two wildcards, a null, a manifest file",
        manifest = _CASES[0].manifest,
        files = _CASES[0].files,
        expected = {"./client": "client.d.ts", "./internal": "dist/node/internal.d.ts", "./module-runner": "dist/node/module-runner.d.ts"},
    ),
    struct(
        package = "web-streams-polyfill@3.3.3",
        shape = "no `exports`; typesVersions maps dist/types/* to dist/types/ts3.6/*, and the map is not a designation",
        manifest = """{
          "name": "web-streams-polyfill",
          "version": "3.3.3",
          "main": "dist/polyfill",
          "browser": "dist/polyfill.min.js",
          "module": "dist/polyfill.mjs",
          "types": "dist/types/polyfill.d.ts",
          "typesVersions": {
            ">=3.6": {
              "dist/types/*": [
                "dist/types/ts3.6/*"
              ]
            }
          }
        }""",
        files = ["dist/types/polyfill.d.ts", "dist/types/ponyfill.d.ts", "dist/types/ts3.6/polyfill.d.ts", "dist/types/ts3.6/ponyfill.d.ts"],
        expected = {},
    ),
]

# The one-star `exports` patterns a manifest designates. `files` may hold no
# declaration under a pattern's directory: the check is on the directory before the star.
_PATTERN_CASES = [
    struct(
        package = "unenv@2.0.0-rc.24",
        shape = "`./*` into dist/runtime with a .d.mts suffix; a starred key whose target has no star",
        manifest = """{
          "name": "unenv",
          "version": "2.0.0-rc.24",
          "type": "module",
          "exports": {
            ".": {
              "types": "./dist/index.d.mts",
              "default": "./dist/index.mjs"
            },
            "./package.json": "./package.json",
            "./mock/proxy-cjs": {
              "types": "./lib/mock.d.cts",
              "default": "./lib/mock.cjs"
            },
            "./mock/proxy-cjs/*": {
              "types": "./lib/mock.d.cts",
              "default": "./lib/mock.cjs"
            },
            "./*": {
              "types": "./dist/runtime/*.d.mts",
              "default": "./dist/runtime/*.mjs"
            }
          },
          "types": "./dist/index.d.mts"
        }""",
        files = ["dist/index.d.mts", "dist/runtime/node/path.d.mts", "lib/mock.d.cts"],
        expected = {"./*": "dist/runtime/*.d.mts", "./mock/proxy-cjs/*": "lib/mock.d.cts"},
    ),
    struct(
        package = "@modelcontextprotocol/sdk@1.28.0",
        shape = "`import` and `require` only: the ESM build's directory, the declaration beside each .js",
        manifest = """{
          "name": "@modelcontextprotocol/sdk",
          "version": "1.28.0",
          "exports": {
            ".": {
              "import": "./dist/esm/index.js",
              "require": "./dist/cjs/index.js"
            },
            "./client": {
              "import": "./dist/esm/client/index.js",
              "require": "./dist/cjs/client/index.js"
            },
            "./server": {
              "import": "./dist/esm/server/index.js",
              "require": "./dist/cjs/server/index.js"
            },
            "./validation": {
              "import": "./dist/esm/validation/index.js",
              "require": "./dist/cjs/validation/index.js"
            },
            "./validation/ajv": {
              "import": "./dist/esm/validation/ajv-provider.js",
              "require": "./dist/cjs/validation/ajv-provider.js"
            },
            "./validation/cfworker": {
              "import": "./dist/esm/validation/cfworker-provider.js",
              "require": "./dist/cjs/validation/cfworker-provider.js"
            },
            "./experimental": {
              "import": "./dist/esm/experimental/index.js",
              "require": "./dist/cjs/experimental/index.js"
            },
            "./experimental/tasks": {
              "import": "./dist/esm/experimental/tasks/index.js",
              "require": "./dist/cjs/experimental/tasks/index.js"
            },
            "./*": {
              "import": "./dist/esm/*",
              "require": "./dist/cjs/*"
            }
          }
        }""",
        files = ["dist/cjs/server/mcp.d.ts", "dist/cjs/types.d.ts", "dist/esm/server/mcp.d.ts", "dist/esm/types.d.ts"],
        expected = {"./*": "dist/esm/*"},
    ),
    struct(
        package = "diff@8.0.3",
        shape = "a suffix on the key itself, and `types` two conditions down; a trailing-slash key is no pattern",
        manifest = """{
          "name": "diff",
          "version": "8.0.3",
          "main": "./libcjs/index.js",
          "types": "libcjs/index.d.ts",
          "exports": {
            ".": {
              "import": { "types": "./libesm/index.d.ts", "default": "./libesm/index.js" },
              "require": { "types": "./libcjs/index.d.ts", "default": "./libcjs/index.js" }
            },
            "./package.json": "./package.json",
            "./lib/*.js": {
              "import": { "types": "./libesm/*.d.ts", "default": "./libesm/*.js" },
              "require": { "types": "./libcjs/*.d.ts", "default": "./libcjs/*.js" }
            },
            "./lib/": {
              "import": { "types": "./libesm/", "default": "./libesm/" },
              "require": { "types": "./libcjs/", "default": "./libcjs/" }
            }
          }
        }""",
        files = ["libcjs/index.d.ts", "libesm/index.d.ts"],
        expected = {"./lib/*.js": "libesm/*.d.ts"},
    ),
    struct(
        package = "vite@8.2.2",
        shape = "two patterns mapping a directory to itself, one to `null`",
        manifest = """{
          "name": "vite",
          "version": "8.2.2",
          "exports": {
            ".": "./dist/node/index.js",
            "./client": { "types": "./client.d.ts" },
            "./module-runner": "./dist/node/module-runner.js",
            "./internal": "./dist/node/internal.js",
            "./dist/client/*": "./dist/client/*",
            "./types/*": { "types": "./types/*" },
            "./types/internal/*": null,
            "./package.json": "./package.json"
          }
        }""",
        files = ["client.d.ts", "dist/client/client.mjs", "dist/node/index.d.ts", "types/hot.d.ts"],
        expected = {"./dist/client/*": "dist/client/*", "./types/*": "types/*"},
    ),
]

def _shipping(files):
    # A directory is shipped when a listed file sits under it, as the repository
    # rule's predicate answers: a pattern is checked at the directory before its star.
    def has_file(path):
        for f in files:
            if f == path or f.startswith(path + "/"):
                return True
        return False

    return has_file

def _module_of(case):
    return getattr(case, "module", case.expected)

def _published_shapes_test(ctx):
    env = unittest.begin(ctx)

    for case in _CASES:
        asserts.equals(
            env,
            case.expected,
            exports_types(json.decode(case.manifest), _shipping(case.files)),
            "{}: {}".format(case.package, case.shape),
        )
        asserts.equals(
            env,
            _module_of(case),
            module_entry(json.decode(case.manifest), _shipping(case.files)),
            "{}, the module entry: {}".format(case.package, case.shape),
        )

    return unittest.end(env)

published_shapes_test = unittest.make(_published_shapes_test)

def _published_subpaths_test(ctx):
    env = unittest.begin(ctx)

    for case in _SUBPATH_CASES:
        asserts.equals(
            env,
            case.expected,
            exports_subpath_types(json.decode(case.manifest), _shipping(case.files)),
            "{}: {}".format(case.package, case.shape),
        )

    return unittest.end(env)

published_subpaths_test = unittest.make(_published_subpaths_test)

def _stanza_for(case, patterns):
    package_name, _, version = case.package.rpartition("@")
    return package_stanza(
        struct(package = package_name, version = version, peer_id = "", types_dep = "", is_types_package = False),
        "pkg",
        package_name,
        "",
        "",
        "",
        {},
        patterns,
        {},
    )

def _published_patterns_test(ctx):
    env = unittest.begin(ctx)

    for case in _PATTERN_CASES:
        asserts.equals(
            env,
            case.expected,
            exports_subpath_patterns(json.decode(case.manifest), _shipping(case.files)),
            "{}: {}".format(case.package, case.shape),
        )

    unenv = _PATTERN_CASES[0]
    stanza = _stanza_for(unenv, exports_subpath_patterns(json.decode(unenv.manifest), _shipping(unenv.files)))
    asserts.true(
        env,
        '    subpath_patterns = {\n        "./*": "dist/runtime/*.d.mts",\n        "./mock/proxy-cjs/*": "lib/mock.d.cts",\n    },\n' in stanza,
        "the stanza carries the attribute as written: " + stanza,
    )
    asserts.true(
        env,
        "subpath_patterns" not in _stanza_for(unenv, {}),
        "and a manifest with no pattern does not name the attribute",
    )

    return unittest.end(env)

published_patterns_test = unittest.make(_published_patterns_test)

# Resolved against this package because the one the BUILD file is generated into
# exists only inside a fetch; whether a string names a path or a repository is
# the same question in either.
_GENERATED_PACKAGE = Label("//tests/npm:BUILD.bazel")

def _written_attribute(stanza, attribute):
    prefix = "    {} = ".format(attribute)
    for line in stanza.split("\n"):
        if line.startswith(prefix):
            return line[len(prefix):].removesuffix(",")
    return ""

def _written_form_test(ctx):
    env = unittest.begin(ctx)

    for case in _CASES:
        if not case.expected and not _module_of(case):
            continue
        package_name, _, version = case.package.rpartition("@")
        stanza = package_stanza(
            struct(package = package_name, version = version, peer_id = "", types_dep = "", is_types_package = False),
            "pkg",
            package_name,
            "",
            exports_types(json.decode(case.manifest), _shipping(case.files)),
            module_entry(json.decode(case.manifest), _shipping(case.files)),
            {},
            {},
            {},
        )

        # node_modules/<name> is what TypeScript reads to take a `paths` match
        # for a library file rather than project source; every label carries it.
        root = "node_modules/" + package_name
        if case.expected:
            asserts.equals(
                env,
                root + "/" + case.expected,
                _GENERATED_PACKAGE.relative(_written_attribute(stanza, "exports_types").strip('"')).name,
                "{}: exports_types names a file under the package root, not a repository".format(case.package),
            )
        else:
            asserts.equals(
                env,
                "",
                _written_attribute(stanza, "exports_types"),
                "{}: no declaration, so no exports_types".format(case.package),
            )
        asserts.equals(
            env,
            root + "/" + _module_of(case),
            _GENERATED_PACKAGE.relative(_written_attribute(stanza, "module_entry").strip('"')).name,
            "{}: module_entry names the file a bare import resolves to, under the package root".format(case.package),
        )
        asserts.equals(
            env,
            '":{}/package.json"'.format(root),
            _written_attribute(stanza, "package_dir"),
            "{}: package_dir is the manifest under the package root".format(case.package),
        )
        asserts.equals(
            env,
            'glob(["{}/**/*"], exclude_directories = 1, allow_empty = True)'.format(root),
            _written_attribute(stanza, "package_files"),
            "{}: package_files globs the package root and nothing beside it".format(case.package),
        )

    return unittest.end(env)

written_form_test = unittest.make(_written_form_test)

def exports_types_test_suite(name):
    unittest.suite(name, published_shapes_test, published_subpaths_test, published_patterns_test, written_form_test)

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
    entry = npm.module_entry_file
    resolved = "{}@{} -> {}".format(
        npm.package_name,
        npm.package_version,
        entry.path if entry else "no module entry",
    )

    asserts.true(env, entry != None, "the package designates a module entry: " + resolved)
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
        "tsgo is pointed at the entry itself, not a directory: {} vs {}".format(mapped[0], resolved),
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
