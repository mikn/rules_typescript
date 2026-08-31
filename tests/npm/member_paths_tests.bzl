"""What a workspace member's package.json says each of its specifiers resolves to.

The precedence is TypeScript's own, not one invented here: `exports` first,
because a package that ships one has declared there which files it means to be
entered through; then `typings` and `types`, in that order, which is how
readPackageJsonTypesFields reads them; then `main`, answered with the declaration
beside it. `module` is not read at all -- it is a bundler convention, and no
TypeScript resolution mode consults it, `bundler` included.

The `exports` shapes are read by the same two functions npm_import uses for a
published tarball, so the two layers cannot come to different conclusions about
one manifest. What differs is what a designated target MEANS: a tarball ships the
file, a member ships the source Bazel has yet to compile a declaration from.
"""

load("@bazel_skylib//lib:unittest.bzl", "asserts", "unittest")
load("//npm/private:member_paths.bzl", "declared_paths", "member_module_paths", "target_offset")

_CASES = [
    struct(
        shape = "an exports map with a subpath per component",
        manifest = {"exports": {
            ".": "./index.ts",
            "./button": "./components/controls/button/index.ts",
        }},
        expected = {
            "": ["index.d.ts"],
            "/button": ["components/controls/button/index.d.ts"],
        },
    ),
    struct(
        shape = "conditions, in the order the map itself writes them",
        manifest = {"exports": {".": {
            "types": "./src/index.d.ts",
            "import": "./dist/index.mjs",
        }}},
        expected = {"": ["src/index.d.ts", "dist/index.d.mts"]},
    ),
    struct(
        shape = "the same conditions written the other way round",
        manifest = {"exports": {".": {
            "import": "./dist/index.mjs",
            "types": "./src/index.d.ts",
        }}},
        expected = {"": ["dist/index.d.mts", "src/index.d.ts"]},
    ),
    struct(
        shape = "a bare string, npm's shorthand for a package exporting itself",
        manifest = {"exports": "./entry.tsx"},
        expected = {"": ["entry.d.ts"]},
    ),
    struct(
        shape = "the condition shorthand, with no subpath keys at all",
        manifest = {"exports": {"default": "./entry.ts"}},
        expected = {"": ["entry.d.ts"]},
    ),
    struct(
        shape = "a fallback array",
        manifest = {"exports": {".": ["./first.ts", "./second.ts"]}},
        expected = {"": ["first.d.ts", "second.d.ts"]},
    ),
    struct(
        shape = "a wildcard subpath, which is how a whole directory is exported",
        manifest = {"exports": {
            "./*": "./components/*.tsx",
            "./hooks/*": "./hooks/*.ts",
        }},
        expected = {
            "/*": ["components/*.d.ts"],
            "/hooks/*": ["hooks/*.d.ts"],
        },
    ),
    struct(
        shape = "a subpath excluded with null designates nothing",
        manifest = {"exports": {
            ".": "./index.ts",
            "./internal/*": None,
        }},
        expected = {"": ["index.d.ts"]},
    ),
    struct(
        shape = "a target no compiler emits a declaration from",
        manifest = {"exports": {
            ".": "./index.ts",
            "./theme.css": "./theme.css",
            "./logo": "./logo.svg",
        }},
        expected = {"": ["index.d.ts"]},
    ),
    struct(
        shape = "two asterisks have no paths pattern",
        manifest = {"exports": {"./*/*": "./src/*/*.ts"}},
        expected = {},
    ),
    struct(
        shape = "a starred value under an unstarred key has nothing to substitute",
        manifest = {"exports": {"./all": "./src/*.ts"}},
        expected = {},
    ),
    struct(
        shape = "no exports: main, which is where most of npm publishes",
        manifest = {"main": "./schema.ts"},
        expected = {"": ["schema.d.ts"]},
    ),
    struct(
        shape = "typings before types, as readPackageJsonTypesFields reads them",
        manifest = {
            "typings": "./legacy.d.ts",
            "types": "./modern.d.ts",
            "main": "./index.js",
        },
        expected = {"": ["legacy.d.ts"]},
    ),
    struct(
        shape = "types beats main",
        manifest = {"types": "./src/index.d.ts", "main": "./dist/index.js"},
        expected = {"": ["src/index.d.ts"]},
    ),
    struct(
        shape = "module is not read: TypeScript does not consult it",
        manifest = {"module": "./esm/index.ts"},
        expected = {},
    ),
    struct(
        shape = "an extensionless types field is the file and then the directory",
        manifest = {"types": "./src/index"},
        expected = {"": ["src/index.d.ts", "src/index/index.d.ts"]},
    ),
    struct(
        shape = "an exports map that designates nothing falls through to main",
        manifest = {"exports": {".": "./styles.css"}, "main": "./index.ts"},
        expected = {"": ["index.d.ts"]},
    ),
    struct(
        shape = "a member that declares nothing keeps the guesses it had",
        manifest = {"name": "shared", "version": "0.0.0"},
        expected = {},
    ),
]

def _member_module_paths_test(ctx):
    env = unittest.begin(ctx)
    for case in _CASES:
        asserts.equals(
            env,
            case.expected,
            member_module_paths(case.manifest),
            case.shape,
        )
    return unittest.end(env)

member_module_paths_test = unittest.make(_member_module_paths_test)

# A member whose entry sits in a subdirectory is compiled by a target in that
# subdirectory, so the roots TsModuleInfo reports are that subdirectory and a
# member-relative declaration would name it twice. A declaration outside it
# belongs to another target and is dropped rather than misnamed.
_OFFSET_CASES = [
    struct(
        shape = "the target is the member's own directory",
        member_dir = "packages/foo",
        package = "packages/foo",
        module_paths = {"": ["index.d.ts"], "/button": ["button/index.d.ts"]},
        expected = [
            struct(specifier = "", declarations = ("index.d.ts",)),
            struct(specifier = "/button", declarations = ("button/index.d.ts",)),
        ],
    ),
    struct(
        shape = "the target is the entry's subdirectory, so paths lose that prefix",
        member_dir = "packages/foo",
        package = "packages/foo/src",
        module_paths = {"": ["src/index.d.ts"], "/button": ["src/button/index.d.ts"]},
        expected = [
            struct(specifier = "", declarations = ("index.d.ts",)),
            struct(specifier = "/button", declarations = ("button/index.d.ts",)),
        ],
    ),
    struct(
        shape = "a declaration outside that subdirectory is another target's",
        member_dir = "packages/foo",
        package = "packages/foo/src",
        module_paths = {"": ["src/index.d.ts"], "/docs": ["docs/index.d.ts"]},
        expected = [struct(specifier = "", declarations = ("index.d.ts",))],
    ),
]

def _declared_paths_test(ctx):
    env = unittest.begin(ctx)
    for case in _OFFSET_CASES:
        asserts.equals(
            env,
            case.expected,
            list(declared_paths(
                case.module_paths,
                target_offset(case.member_dir, case.package),
            )),
            case.shape,
        )
    return unittest.end(env)

declared_paths_test = unittest.make(_declared_paths_test)

def member_paths_test_suite(name):
    unittest.suite(name, declared_paths_test, member_module_paths_test)
