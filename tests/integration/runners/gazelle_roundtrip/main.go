package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/mikn/rules_typescript/tests/integration/harness"
)

// A deleted test target satisfies both "the output builds" and "two runs are
// byte-identical", which is how seven hand-written go_tests once disappeared.
func testTargets(it *harness.IT) []string {
	out := strings.Fields(it.BazelStdout("query", "tests(//...)"))
	sort.Strings(out)
	return out
}

func main() {
	harness.Run(harness.Config{
		Name:         "gazelle_roundtrip",
		WorkspaceRel: "tests/integration/gazelle_roundtrip",
		Lockfile:     "tests/npm/pnpm-lock.yaml",
	}, func(it *harness.IT) {
		dirs := []string{"src/lib", "src/app", "src/icons", "src/typed", "worker", "worker/src", "worker/test", "worker/test/deep", "generated_worker/src", "aliased", "aliased/shared", "aliased/src"}

		// Written here rather than shipped in the workspace: a BUILD file under
		// a --deleted_packages entry is a package of the OUTER workspace, and
		// glob_workspace_files would stop collecting the fixture below it --
		// the same boundary this case is about.
		it.Write(it.Path("src/i18n/BUILD.bazel"), codegenGlobDirective)
		it.Write(it.Path("src/locales/BUILD.bazel"), outDirCodegenPackage)
		it.Write(it.Path("generated_worker/BUILD.bazel"), generatedWorkerPackage)
		it.Write(it.Path("devserver/BUILD.bazel"), handWrittenDevServerPackage)
		// The harness stages one lockfile, vitest's; wrangler is in tests/workers'.
		it.Write(it.Path("pnpm-lock.workers.yaml"), it.Read(filepath.Join(it.RulesTSRoot, "tests/workers/pnpm-lock.yaml")))

		gazelleLog, err := it.BazelLog("gazelle_pass_1", "run", "//:gazelle", "--", "-ts_verbose")
		if err != nil {
			gazelleLog.Dump()
			it.Fail("bazel run //:gazelle exited non-zero: %v", err)
		}
		it.Pass("gazelle pass 1 complete")

		for _, dir := range dirs {
			it.RequireFile(it.Path(dir, "BUILD.bazel"), "Gazelle did not generate %s/BUILD.bazel", dir)
			it.Pass("%s/BUILD.bazel generated (pass 1)", dir)
		}

		devServerAfterFirstRun := it.Read(it.Path("devserver/BUILD.bazel"))

		before := testTargets(it)
		if len(before) == 0 {
			it.Fail("no test targets in this workspace: the set comparison below would be vacuous")
		}
		it.Pass("test targets after pass 1: %d", len(before))

		firstPass := map[string]string{}
		for _, dir := range dirs {
			firstPass[dir] = it.Read(it.Path(dir, "BUILD.bazel"))
			if err := os.Remove(it.Path(dir, "BUILD.bazel")); err != nil {
				it.Fail("cannot delete %s/BUILD.bazel: %v", dir, err)
			}
		}
		it.Pass("BUILD files snapshotted and deleted")

		it.MustBazel("run", "//:gazelle")
		it.Pass("gazelle pass 2 complete")

		differs := false
		for _, dir := range dirs {
			second := it.Read(it.Path(dir, "BUILD.bazel"))
			if second != firstPass[dir] {
				fmt.Fprintf(os.Stderr, "--- %s/BUILD.bazel pass 1 ---\n%s--- pass 2 ---\n%s", dir, firstPass[dir], second)
				differs = true
				continue
			}
			it.Pass("%s/BUILD.bazel is identical across both Gazelle runs", dir)
		}
		if differs {
			it.Fail("Gazelle output is not idempotent — see the dumps above")
		}

		it.MustBazel("build", "//...")
		it.Pass("bazel build //...")

		after := testTargets(it)
		if strings.Join(before, "\n") != strings.Join(after, "\n") {
			fmt.Fprintf(os.Stderr, "--- before ---\n%s\n--- after ---\n%s\n",
				strings.Join(before, "\n"), strings.Join(after, "\n"))
			it.Fail("Gazelle changed the test target set: %d before, %d after", len(before), len(after))
		}
		it.Pass("test target set unchanged across a delete-and-regenerate: %d", len(after))

		it.MustBazel("test", "//...")
		it.Pass("bazel test //... on Gazelle's own output")

		for _, rel := range []string{"src/lib/math.js", "src/lib/math.d.ts", "src/app/index.js", "src/app/index.d.ts"} {
			it.RequireFile(it.Bin(rel), "expected output file not found: %s", rel)
			it.Pass("output file exists: %s", rel)
		}

		rootAmbientTypesReachTheTreeBelow(it)
		generatedDeclarationNamesItsGenerator(it)
		aliasAttrsFollowTheSrcs(it)
		codegenGlobLoads(it)
		outDirContentsAreNotSources(it)
		assetDeclarationTypeApplies(it)
		anchoredExcludeHitsOnePath(it)
		jsSrcsAreCompiledAndDeclared(it)
		checkedInDeclarationTypesTheJavaScript(it)
		lintWithoutTheLinterIsRefused(it, gazelleLog)
		programsAreListedWithTheToolchainsTsgo(it, gazelleLog)
		handWrittenDevServerIsLeftAlone(it, devServerAfterFirstRun)
		// Last: it rewrites the tree the checks above read.
		declarationMovesToACodegen(it)
	})
}

// Gazelle writes no ts_dev_server and knows no such kind; beside a main.ts and
// an index.html, this is the only dev target the package has.
const handWrittenDevServerRule = `ts_dev_server(
    name = "dev",
    entry_point = ":devserver",
    port = 5173,
)
`

const handWrittenDevServerPackage = `load("@rules_typescript//ts:defs.bzl", "ts_dev_server")

` + handWrittenDevServerRule

// The converge test says the text survives a run; only a nested Bazel says the
// rule still loads and builds against the ts_compile Gazelle wrote beside it.
func handWrittenDevServerIsLeftAlone(it *harness.IT, afterFirstRun string) {
	build := it.Path("devserver/BUILD.bazel")
	second := it.Read(build)
	if second != afterFirstRun {
		fmt.Fprintf(os.Stderr, "--- devserver/BUILD.bazel pass 1 ---\n%s--- pass 2 ---\n%s", afterFirstRun, second)
		it.Fail("the package holding the hand-written ts_dev_server changed between two Gazelle runs")
	}
	it.Pass("devserver/BUILD.bazel is identical across both Gazelle runs")

	it.RequireContains(build, handWrittenDevServerRule,
		"the hand-written ts_dev_server did not come through the Gazelle runs verbatim")
	it.RequireContains(build, `"ts_dev_server"`,
		"the load statement lost the ts_dev_server symbol")
	it.RequireContains(build, `name = "devserver"`,
		"Gazelle wrote no ts_compile for main.ts beside the hand-written rule")
	it.Pass("the ts_dev_server, its load symbol and the ts_compile beside it are all in devserver/BUILD.bazel")

	it.RequireFile(it.Bin("devserver/dev_launcher"),
		"//devserver:dev did not build; the `bazel build //...` above wrote no launcher for it")
	it.Pass("//devserver:dev built against the ts_compile Gazelle wrote beside it")
}

// The hand-written target the migration below appends to worker/BUILD.bazel,
// without its load line: the Gazelle run adds the symbol to the file's load.
const workerTypesCodegen = `
ts_codegen(
    name = "worker_types",
    srcs = ["bindings.txt"],
    outs = ["worker-configuration.d.ts"],
    args = [
        "--bindings",
        "{srcs}",
        "--out",
        "{out}",
    ],
    generator = "//:env_types_gen",
    visibility = ["//worker:__subpackages__"],
)
`

// Bazel says the half the converge test cannot: the rewritten labels resolve,
// and every program under worker/ type-checks against the generated declaration.
func declarationMovesToACodegen(it *harness.IT) {
	if err := os.Remove(it.Path("worker/worker-configuration.d.ts")); err != nil {
		it.Fail("cannot delete the checked-in declaration: %v", err)
	}
	it.Write(it.Path("worker/bindings.txt"), "bucket\n")
	owner := it.Path("worker/BUILD.bazel")
	it.Write(owner, it.Read(owner)+workerTypesCodegen)
	it.MustBazel("run", "//:gazelle")
	it.Pass("gazelle run over the migration: declaration deleted, ts_codegen appended")

	it.RequireContains(owner, `name = "worker_types"`,
		"the hand-written ts_codegen did not survive the Gazelle run")
	it.RequireNotContains(owner, "tsconfig_types",
		"the filegroup outlived the file it staged")
	it.RequireNotContains(owner, "ts_compile(",
		"the ts_compile whose only src was the deleted declaration survived")
	it.Pass("worker/BUILD.bazel keeps the ts_codegen and loses the filegroup and the ts_compile")

	for _, dir := range []string{"worker/src", "worker/test", "worker/test/deep"} {
		build := it.Path(dir, "BUILD.bazel")
		it.RequireNotContains(build, "tsconfig_types",
			"//%s still names the withdrawn filegroup", dir)
		it.RequireContains(build, `types_srcs = ["//worker:worker_types"]`,
			"//%s does not name the ts_codegen staging the declaration", dir)
		it.Pass("//%s names the ts_codegen where it named the filegroup", dir)
	}

	it.MustBazel("test", "//worker/...")
	it.Pass("bazel test //worker/...: every program under worker/ resolves the generated declaration")
	declaration := it.Bin("worker/worker-configuration.d.ts")
	it.RequireContains(declaration, "readonly bucket: string;",
		"the generated declaration names no binding from bindings.txt")
	it.Pass("the declaration the programs compiled against is the generated one")
}

// Every tsconfig.json here is below the root, so tsgo lists each; that the tsgo
// is the toolchain's, out of the binary's runfiles, only a consumer's run can say.
func programsAreListedWithTheToolchainsTsgo(it *harness.IT, gazelleLog *harness.Log) {
	binary := regexp.MustCompile(`listing programs with \S+/ts/toolchain/tsgo_resolved/tsgo \(runfiles\)`).FindString(gazelleLog.Text)
	if binary == "" {
		gazelleLog.Dump()
		it.Fail("Gazelle did not find tsgo through the runfiles")
	}
	it.Pass("Gazelle lists programs with the toolchain's tsgo from its runfiles: %s", binary)

	for _, dir := range []string{"src/app", "src/locales", "aliased", "worker", "worker/test", "worker/test/deep", "generated_worker"} {
		if !gazelleLog.Matches(dir + `/tsconfig.json: \d+ files listed, [1-9]\d* roots, \d+ edges, \d+ type entries`) {
			gazelleLog.Dump()
			it.Fail("Gazelle did not list %s/tsconfig.json", dir)
		}
		it.Pass("%s/tsconfig.json listed", dir)
	}
	summary := regexp.MustCompile(`\d+ \.ts/\.tsx/\.mts/\.cts files in no program across \d+ director(y|ies)`).FindString(gazelleLog.Text)
	if summary == "" {
		gazelleLog.Dump()
		it.Fail("Gazelle did not report the files in no program once the walk was done")
	}
	it.Pass("Gazelle reports the files in no program: %s", summary)
}

// src/app holds an eslint.config.js, and tests/npm's lockfile has oxlint and no
// eslint. A ts_lint naming @npm//:eslint_bin is a label the hub does not
// declare, and `no such target` fails the whole package at analysis -- so the
// `bazel build //...` above is the half of this that only Bazel can say.
func lintWithoutTheLinterIsRefused(it *harness.IT, gazelleLog *harness.Log) {
	it.RequireNotContains(it.Path("src/app/BUILD.bazel"), "ts_lint",
		"src/app got a ts_lint for a linter the lockfile does not have")
	it.Pass("src/app/BUILD.bazel carries no ts_lint")

	if !gazelleLog.Contains("src/app/eslint.config.js") {
		gazelleLog.Dump()
		it.Fail("Gazelle wrote no ts_lint for src/app and did not say which config it refused")
	}
	it.Pass("Gazelle named src/app/eslint.config.js as the config it wrote no ts_lint for")
}

const codegenGlobDirective = "# gazelle:ts_codegen locales //:catalogue_gen locales.ts " +
	"srcs:settings.json,glob([\"messages/*.json\"]) {srcs} {out}\n"

// A hand-written out_dir ts_codegen in a tsconfig-mode package, with a tree
// checked in under compiled/ standing in for what a local run leaves behind.
const outDirCodegenPackage = `# gazelle:ts_package_boundary tsconfig

load("@rules_typescript//ts:defs.bzl", "ts_codegen")

ts_codegen(
    name = "tree",
    srcs = ["names.txt"],
    args = [
        "--names",
        "{srcs}",
        "--outdir",
        "{out}",
    ],
    generator = "//:tree_gen",
    module_name = "@roundtrip/locales",
    out_dir = "compiled",
)
`

// A worker with nothing checked in: the ts_codegen beside the config writes the
// declaration its tsconfig names.
const generatedWorkerPackage = `load("@rules_typescript//npm:defs.bzl", "node_modules")
load("@rules_typescript//ts:defs.bzl", "ts_codegen")

node_modules(
    name = "node_modules",
    deps = ["@npm_workers//:wrangler"],
)

ts_codegen(
    name = "worker_types",
    srcs = ["wrangler.jsonc"],
    outs = ["worker-configuration.d.ts"],
    args = [
        "--config",
        "wrangler.jsonc",
        "--out",
        "{out}",
        "--srcs",
        "{srcs}",
        "--strict-vars=false",
    ],
    generator = "@rules_typescript//tools/codegen:wrangler_types",
    node_modules = ":node_modules",
    visibility = ["//generated_worker:__subpackages__"],
)
`

// The converge test says Gazelle names the ts_codegen for a declaration not on
// disk; only a build says the program then type-checks against what it wrote.
func generatedDeclarationNamesItsGenerator(it *harness.IT) {
	owner := it.Path("generated_worker/BUILD.bazel")
	it.RequireContains(owner, `name = "worker_types"`,
		"the hand-written ts_codegen did not survive the Gazelle run")
	it.RequireNotContains(owner, "tsconfig_types",
		"a filegroup was written over a file that is not in the source tree")
	it.Pass("generated_worker/BUILD.bazel keeps the ts_codegen and gets no filegroup")

	below := it.Path("generated_worker/src/BUILD.bazel")
	it.RequireContains(below, `types = ["../worker-configuration.d.ts"]`,
		"//generated_worker/src names no rebased types entry")
	it.RequireContains(below, `types_srcs = ["//generated_worker:worker_types"]`,
		"//generated_worker/src does not name the ts_codegen staging the declaration")
	it.Pass("//generated_worker/src carries the rebased entry and the ts_codegen's label")

	declaration := it.Bin("generated_worker/worker-configuration.d.ts")
	it.RequireFile(declaration, "the wrangler types action did not run")
	it.RequireContains(declaration, "CACHE: KVNamespace;",
		"the generated declaration names no binding from wrangler.jsonc")
	it.Pass("wrangler types wrote the declaration from wrangler.jsonc, with no checked-in copy")
	it.RequireFile(it.Bin("generated_worker/src/index.js"),
		"//generated_worker/src did not compile against the generated declaration")
	it.Pass("//generated_worker/src type-checks against the Env and runtime globals the generated file declares")

	// A binding the config does not declare; only the generated Env can refuse it.
	consumer := it.Path("generated_worker/src/index.ts")
	restore := it.Read(consumer)
	it.Write(consumer, strings.Replace(restore, "env.GREETING", "env.MISSING", 1))
	log, err := it.BazelLog("generated_env_is_in_force", "build", "//generated_worker/src")
	it.Write(consumer, restore)
	if err == nil {
		log.Dump()
		it.Fail("//generated_worker/src compiled a binding wrangler.jsonc does not declare; the generated Env is not the type in force")
	}
	if !log.Contains("TS2339") {
		log.Dump()
		it.Fail("//generated_worker/src failed for some other reason than the undeclared binding")
	}
	it.Pass("env.MISSING is TS2339, so the generated Env types the worker")
}

// A file under the out_dir read into srcs is one output declared twice, the tree
// and a file inside it; the `bazel build //...` above is where Bazel says so.
func outDirContentsAreNotSources(it *harness.IT) {
	build := it.Path("src/locales/BUILD.bazel")
	it.RequireNotContains(build, "compiled/",
		"src/locales/BUILD.bazel names a file under the out_dir of :tree")
	it.Pass("src/locales/BUILD.bazel names nothing under compiled/")

	it.RequireContains(build, `deps = [":tree"]`,
		"the import of @roundtrip/locales did not resolve to the out_dir target")
	it.Pass("the import of :tree's module_name resolves to :tree")

	for _, dir := range []string{"src/locales/compiled", "src/locales/compiled/messages"} {
		it.RequireNoFile(it.Path(dir, "BUILD.bazel"), "Gazelle made %s a package inside :tree's out_dir", dir)
	}
	it.Pass("no package under src/locales/compiled")

	it.RequireFile(it.Bin("src/locales/app.js"), "//src/locales did not compile against the tree")
	it.Pass("//src/locales compiles against the generated tree")
}

// The converge tests assert generated BUILD text; nothing there asks Bazel to
// load it. A ts_codegen srcs glob reaching into a subdirectory is the case
// where the text can be right and the package still not parse.
func codegenGlobLoads(it *harness.IT) {
	subpkg := it.Path("src/i18n/messages/BUILD.bazel")
	it.RequireNoFile(subpkg, "Gazelle made src/i18n/messages a package; //src/i18n's glob cannot see into one")
	it.Pass("src/i18n/messages has no BUILD file of its own")

	it.RequireContains(it.Path("src/i18n/BUILD.bazel"),
		`srcs = ["settings.json"] + glob(["messages/*.json"])`,
		"the directive's srcs did not reach src/i18n/BUILD.bazel as Starlark")
	it.Pass("src/i18n/BUILD.bazel carries the directive's srcs as Starlark")

	generated := it.Bin("src/i18n/locales.ts")
	it.RequireFile(generated, "the codegen action did not run")
	for _, name := range []string{"settings.json", "en.json", "sv.json"} {
		it.RequireContains(generated, name, "the codegen action never saw %s", name)
		it.Pass("the codegen action saw %s", name)
	}

	it.Write(subpkg, "# Makes src/i18n/messages a package.\n")
	log, err := it.BazelLog("glob_across_a_package", "query", "//src/i18n:locales")
	if err := os.Remove(subpkg); err != nil {
		it.Fail("cannot remove the probe BUILD file: %v", err)
	}
	if err == nil {
		log.Dump()
		it.Fail("//src/i18n loaded with messages/ a package of its own; the glob is supposed to stop matching")
	}
	if !log.Contains("didn't match anything") {
		log.Dump()
		it.Fail("//src/i18n failed to load for some other reason than the empty glob")
	}
	it.Pass("a BUILD file in messages/ empties //src/i18n's glob and Bazel refuses the package")
}

// A bare ts_exclude pattern matches a basename, so it drops that name at every
// depth below the declaration. The root's "./src/lib/lib.config.ts" is the
// anchored form, and src/app's namesake is what says so: nothing about the
// generated text distinguishes the two spellings in one package alone.
func anchoredExcludeHitsOnePath(it *harness.IT) {
	it.RequireNotContains(it.Path("src/lib/BUILD.bazel"), "lib.config.ts",
		"the anchored exclusion did not drop src/lib/lib.config.ts")
	it.Pass("src/lib/BUILD.bazel names no lib.config.ts")

	it.RequireContains(it.Path("src/app/BUILD.bazel"), "lib.config.ts",
		"the anchored exclusion reached the namesake in src/app")
	it.Pass("src/app/BUILD.bazel still names its own lib.config.ts")

	it.RequireFile(it.Bin("src/app/lib.config.js"),
		"src/app/lib.config.ts is in the srcs but nothing compiled it")
	it.Pass("src/app/lib.config.ts compiles; src/lib's namesake is out of the build")
}

// The converge tests assert generated BUILD text, and a type expression can
// reach the BUILD file intact and still not be the type the import gets. The
// negative probe is the half that says so: an unresolvable expression widens the
// import to `any` under skipLibCheck, and `any` compiles either way.
func assetDeclarationTypeApplies(it *harness.IT) {
	const declared = `".svg": "{ readonly viewBox: string }"`

	it.RequireContains(it.Path("src/icons/BUILD.bazel"), declared,
		"the ts_asset_declaration_type directive did not reach src/icons/BUILD.bazel")
	it.Pass("the directive reached the generated asset_library, spaces intact")

	it.RequireContains(it.Bin("src/icons/logo.svg.d.ts"),
		"declare const asset: { readonly viewBox: string };",
		"the generated declaration does not carry the directive's type")
	it.Pass("logo.svg.d.ts declares the directive's type")

	// //src/icons compiled above, and it reads logo.viewBox: TS2339 on the
	// string default. What is left is proving the type is enforced rather than
	// widened, which only a compile that has to fail can say.
	consumer := it.Path("src/icons/index.ts")
	restore := it.Read(consumer)
	it.Write(consumer, "import logo from \"./logo.svg\";\n\nexport const url: string = logo;\n")
	log, err := it.BazelLog("asset_declaration_type_is_not_a_string", "build", "//src/icons")
	it.Write(consumer, restore)
	if err == nil {
		log.Dump()
		it.Fail("//src/icons compiled with the .svg import assigned to a string; the declared type is not being applied")
	}
	if !log.Contains("TS2322") {
		log.Dump()
		it.Fail("//src/icons failed to build for some other reason than the assignment")
	}
	it.Pass("assigning the .svg import to a string is TS2322, so the declared type is the one in force")
}

// A .mjs admitted by ts_js_srcs is the only src that reaches ts_compile's
// .d.mts emit, and the generated BUILD text says nothing about whether the
// declaration a consumer type-checks against was written at all.
func jsSrcsAreCompiledAndDeclared(it *harness.IT) {
	it.RequireContains(it.Path("src/lib/BUILD.bazel"), "format.mjs",
		"the ts_js_srcs directive did not reach src/lib's srcs")
	it.Pass("src/lib/BUILD.bazel names format.mjs")

	for _, rel := range []string{"src/lib/format.mjs", "src/lib/format.d.mts"} {
		it.RequireFile(it.Bin(rel), "expected output file not found: %s", rel)
		it.Pass("output file exists: %s", rel)
	}

	// //src/app compiled above against ../lib, which re-exports format. What is
	// left is whether the .d.mts is the type in force: without it the import
	// widens to `any` and any use of it compiles.
	consumer := it.Path("src/app/index.ts")
	restore := it.Read(consumer)
	it.Write(consumer, "import { format } from \"../lib\";\n\nexport const n: number = format(1);\n")
	log, err := it.BazelLog("js_srcs_declaration_is_in_force", "build", "//src/app")
	it.Write(consumer, restore)
	if err == nil {
		log.Dump()
		it.Fail("//src/app compiled with format()'s string result assigned to a number; the .d.mts is not being applied")
	}
	if !log.Contains("TS2322") {
		log.Dump()
		it.Fail("//src/app failed to build for some other reason than the assignment")
	}
	it.Pass("assigning format()'s result to a number is TS2322, so the JSDoc type crossed the package boundary")
}

// src/typed is the monorepo shape: an untyped compile.mjs, the compile.d.mts
// that declares it -- tsc pairs the two by name -- and a test importing
// "./compile.mjs". Both files belong to the package target and the test depends
// on it; whether the checked-in declaration is the type in force, rather than
// the `any` tsgo infers from the JavaScript, is what only a compile that has to
// fail can say.
func checkedInDeclarationTypesTheJavaScript(it *harness.IT) {
	build := it.Path("src/typed/BUILD.bazel")
	for _, want := range []string{`"compile.d.mts"`, `"compile.mjs"`} {
		it.RequireContains(build, want, "src/typed/BUILD.bazel does not name %s", want)
	}
	it.Pass("src/typed/BUILD.bazel names the declaration and the JavaScript it declares")
	it.RequireContains(build, `":typed"`, "the test does not depend on the target holding compile.d.mts")
	it.Pass("//src/typed:typed_test depends on //src/typed:typed")

	// compile.d.mts takes a number; the JavaScript takes anything.
	test := it.Path("src/typed/compile.test.ts")
	restore := it.Read(test)
	it.Write(test, "import { compile } from \"./compile.mjs\";\n\nexport const s: string = compile(\"x\");\n")
	log, err := it.BazelLog("checked_in_declaration_is_in_force", "build", "//src/typed:_typed_test_compile")
	it.Write(test, restore)
	if err == nil {
		log.Dump()
		it.Fail("//src/typed:_typed_test_compile compiled compile(\"x\"); the checked-in .d.mts is not the type in force")
	}
	if !log.Contains("TS2345") {
		log.Dump()
		it.Fail("//src/typed:_typed_test_compile failed for some other reason than the argument")
	}
	it.Pass("compile(\"x\") is TS2345, so compile.d.mts types the import, not tsgo's inference from the .mjs")
}

// aliased/tsconfig.json maps #shared/* to ./shared/*, and a test file in each
// of its two subdirectories imports a type through it. The alias a test imports
// through has to be on the ts_test -- the test files are a program of their own
// -- and whether path_alias_srcs goes with it follows the srcs: a test with a
// src under aliased/shared/ validates the alias on that src, and one without
// fails analysis until the attribute names the target that owns the directory.
func aliasAttrsFollowTheSrcs(it *harness.IT) {
	const entry = `"#shared/": "aliased/shared/",`
	const srcsAttr = `path_alias_srcs = ["//aliased/shared"],`

	covered := it.Path("aliased/shared/BUILD.bazel")
	if n := strings.Count(it.Read(covered), entry); n != 1 {
		fmt.Fprint(os.Stderr, it.Read(covered))
		it.Fail("aliased/shared holds a ts_compile importing through no alias and a ts_test importing through #shared/; the alias is on %d rules, want 1", n)
	}
	it.RequireNotContains(covered, "path_alias_srcs",
		"util.test.ts is under aliased/shared/, so the alias validates on the test's own srcs and nothing else needs staging")
	it.Pass("//aliased/shared:shared_test carries the alias and no path_alias_srcs")

	uncovered := it.Path("aliased/src/BUILD.bazel")
	for _, want := range []string{entry, srcsAttr} {
		if n := strings.Count(it.Read(uncovered), want); n != 2 {
			fmt.Fprint(os.Stderr, it.Read(uncovered))
			it.Fail("aliased/src holds a ts_compile and a ts_test, both importing through #shared/ with no src under aliased/shared/; %s is on %d rules, want 2", want, n)
		}
	}
	it.Pass("both rules in //aliased/src carry the alias and path_alias_srcs naming //aliased/shared")

	tests := strings.Fields(it.BazelStdout("query", "kind(ts_test, //aliased/...)"))
	if len(tests) != 2 {
		it.Fail("expected the two generated ts_tests under //aliased, got %v", tests)
	}
	it.MustBazel(append([]string{"test"}, tests...)...)
	it.Pass("%s run, so both test programs resolved #shared/util", strings.Join(tests, " and "))

	// What path_alias_srcs costs, and why it follows the srcs: it stages every
	// output of the target it names, the .js included, where the dep edge alone
	// stages the declarations.
	stagesUtilJS := func(target string) bool {
		out := it.BazelStdout("aquery", "--output=text", fmt.Sprintf("mnemonic(\"Tsgo(Declare|Check)\", %s)", target))
		if !strings.Contains(out, "Mnemonic: Tsgo") {
			it.Fail("no tsgo action on %s, so the input check below would be vacuous:\n%s", target, out)
		}
		return strings.Contains(out, "aliased/shared/util.js")
	}
	if stagesUtilJS("//aliased/shared:_shared_test_compile") {
		it.Fail("the covered test's type-check stages aliased/shared/util.js, which only path_alias_srcs puts there")
	}
	it.Pass("the covered test's type-check stages //aliased/shared's declarations and nothing more")
	if !stagesUtilJS("//aliased/src:_src_test_compile") {
		it.Fail("the uncovered test's type-check does not stage aliased/shared/util.js, so path_alias_srcs did not reach the action")
	}
	it.Pass("the uncovered test's type-check stages every output of //aliased/shared, util.js included")

	// The measurement behind writing path_alias_srcs at all.
	restore := it.Read(uncovered)
	it.Replace(uncovered, "    "+srcsAttr+"\n", "")
	log, err := it.BazelLog("alias_without_srcs", "build", "//aliased/src:src")
	it.Write(uncovered, restore)
	if err == nil {
		log.Dump()
		it.Fail("//aliased/src:src analysed without path_alias_srcs, so Gazelle need not write it")
	}
	if !log.Contains("where none of this target's inputs live") {
		log.Dump()
		it.Fail("//aliased/src:src failed for some other reason than the alias guard")
	}
	it.Pass("without path_alias_srcs the alias fails analysis: nothing //aliased/src stages sits under aliased/shared/")
}

// worker/tsconfig.json states `types: ["./worker-configuration.d.ts"]`, and
// worker/src/handler.ts uses two of the declarations that file makes. The
// generated per-directory config states its own `include`, `files` and
// `exclude` and inherits only compilerOptions, so the entry arrives written
// relative to a directory in bazel-out: Gazelle has to rebase it and name a
// label that stages the file.
func rootAmbientTypesReachTheTreeBelow(it *harness.IT) {
	it.RequireContains(it.Path("worker/BUILD.bazel"), `name = "tsconfig_types"`,
		"the tsconfig's own package supplies no label for the declaration it names")
	it.Pass("worker/BUILD.bazel carries the filegroup staging worker-configuration.d.ts")

	it.RequireNotContains(it.Path("worker/BUILD.bazel"), "public_globals",
		"the declaration was published to consumers, which is the propagating shape")
	it.Pass("nothing publishes the declaration to consumers")

	below := it.Path("worker/src/BUILD.bazel")
	it.RequireContains(below, `types = ["../worker-configuration.d.ts"]`,
		"//worker/src names no rebased types entry")
	it.RequireContains(below, `types_srcs = ["//worker:tsconfig_types"]`,
		"//worker/src names no label staging the declaration")
	it.Pass("//worker/src carries the rebased entry and the label that answers it")

	it.RequireFile(it.Bin("worker/src/handler.js"),
		"//worker/src did not compile, so the declarations never reached its program")
	it.Pass("//worker/src type-checks against declarations nothing there imports")

	everyKindLoads(it, below)
	inheritedEntryResolvesToNothing(it, below)
	theDeclarationStopsAtTheSubtree(it)
	parentEntryNamesTheAncestorsLabel(it)
	packageOnlyListIsWrittenWhole(it)
}

// Writing an attribute is not Bazel accepting it: a ts_test that does not take
// types_srcs is a load error on the package, which generated text cannot show.
func everyKindLoads(it *harness.IT, below string) {
	for _, attr := range []string{`types = ["../worker-configuration.d.ts"]`, `types_srcs = ["//worker:tsconfig_types"]`} {
		if strings.Count(it.Read(below), attr) != 2 {
			fmt.Fprint(os.Stderr, it.Read(below))
			it.Fail("worker/src holds a ts_compile and a ts_test; %s is not on both", attr)
		}
	}
	it.Pass("both rules Gazelle wrote in worker/src carry the pair")

	targets := strings.Fields(it.BazelStdout("query", "kind(ts_test, //worker/src/...)"))
	if len(targets) != 1 {
		it.Fail("expected one generated ts_test under //worker/src, got %v -- the load below would be vacuous", targets)
	}
	it.Pass("Bazel loaded the package Gazelle wrote both attributes into: %s", targets[0])

	it.MustBazel("test", targets[0])
	it.Pass("%s runs, so the test files' own program resolved the entry", targets[0])
}

// The measurement behind writing the entry at all: with the entry only
// inherited through `extends` -- and the file staged on a dep edge, so it is in
// the sandbox -- the name resolves against the generated config's own directory
// and finds nothing.
func inheritedEntryResolvesToNothing(it *harness.IT, below string) {
	restore := it.Read(below)
	it.Write(below, `load("@rules_typescript//ts:defs.bzl", "ts_compile")

ts_compile(
    name = "src",
    srcs = ["handler.ts"],
    tsconfig = "//worker:tsconfig",
    visibility = ["//visibility:public"],
    deps = ["//worker:worker"],
)
`)
	log, err := it.BazelLog("inherited_types_entry", "build", "//worker/src")
	it.Write(below, restore)
	if err == nil {
		log.Dump()
		it.Fail("//worker/src compiled on the inherited entry alone; Gazelle need not write one")
	}
	if !log.Matches(`(?i)TS2304`) {
		log.Dump()
		it.Fail("//worker/src failed for some other reason than the undefined identifiers")
	}
	it.Pass("the entry inherited through extends resolves to nothing: TS2304")

	// So types_srcs alone was never the smaller change: the rule guards a
	// staged file no entry of its own names.
	it.Write(below, `load("@rules_typescript//ts:defs.bzl", "ts_compile")

ts_compile(
    name = "src",
    srcs = ["handler.ts"],
    tsconfig = "//worker:tsconfig",
    types_srcs = ["//worker:tsconfig_types"],
    visibility = ["//visibility:public"],
)
`)
	log, err = it.BazelLog("types_srcs_without_an_entry", "build", "//worker/src")
	it.Write(below, restore)
	if err == nil {
		log.Dump()
		it.Fail("types_srcs with no entry naming the file built, so Gazelle need write no entry")
	}
	if !log.Matches(`which no compilerOptions.types entry names`) {
		log.Dump()
		it.Fail("//worker/src failed for some other reason than the unnamed types_srcs")
	}
	it.Pass("types_srcs with no entry naming the file is an analysis error")
}

// The failure the previous shape had: a declaration reaching every transitive
// consumer. types_srcs stages a file into one program and travels on no edge,
// so a consumer outside the tsconfig's subtree that deps a target inside it
// does not get the declarations.
func theDeclarationStopsAtTheSubtree(it *harness.IT) {
	it.Write(it.Path("outside/ok.ts"), "export const ran = 1;\n")
	it.Write(it.Path("outside/leaked.ts"), "export const seen = WORKER_BUILD_ID;\n")
	it.Write(it.Path("outside/BUILD.bazel"), `load("@rules_typescript//ts:defs.bzl", "ts_compile")

ts_compile(
    name = "ok",
    srcs = ["ok.ts"],
    deps = ["//worker/src"],
)

ts_compile(
    name = "leaked",
    srcs = ["leaked.ts"],
    deps = ["//worker/src"],
)
`)
	it.MustBazel("build", "//outside:ok")
	it.Pass("the dep edge into the subtree analyses and builds")

	log, err := it.BazelLog("declaration_stops_at_the_subtree", "build", "//outside:leaked")
	if err == nil {
		log.Dump()
		it.Fail("the declaration reached a consumer outside the tsconfig's subtree")
	}
	if !log.Matches(`(?i)TS2304`) {
		log.Dump()
		it.Fail("//outside:leaked failed for some other reason than the undefined identifier")
	}
	it.Pass("the same dep edge carries no declaration out of the subtree: TS2304")
}

// worker/test names the worker's declaration as `../` and worker/test/deep as
// `../../`; the one label staging it is the filegroup beside the worker's tsconfig.
func parentEntryNamesTheAncestorsLabel(it *harness.IT) {
	for _, leaf := range []struct{ dir, entry string }{
		{"worker/test", "../worker-configuration.d.ts"},
		{"worker/test/deep", "../../worker-configuration.d.ts"},
	} {
		build := it.Path(leaf.dir, "BUILD.bazel")
		it.RequireContains(build, `types = ["`+leaf.entry+`"]`,
			"//%s names no %s entry", leaf.dir, leaf.entry)
		it.RequireContains(build, `types_srcs = ["//worker:tsconfig_types"]`,
			"//%s names no label of the ancestor staging the declaration", leaf.dir)
		it.RequireNotContains(build, `name = "tsconfig_types"`,
			"%s got a filegroup over a file it does not hold", leaf.dir)
		it.Pass("//%s carries the %s entry and the ancestor's label", leaf.dir, leaf.entry)
	}

	targets := strings.Fields(it.BazelStdout("query", "kind(ts_test, //worker/test/...)"))
	if len(targets) != 2 {
		it.Fail("expected one generated ts_test in each of //worker/test and //worker/test/deep, got %v -- the run below would be vacuous", targets)
	}
	it.MustBazel(append([]string{"test"}, targets...)...)
	it.Pass("%s run, so each test program resolved its entry through //worker:tsconfig_types", strings.Join(targets, " and "))
}

// src/app/tsconfig.json names vite/client and no file; the rule puts a package
// entry's declaration in `files` only when the attribute carries the entry.
func packageOnlyListIsWrittenWhole(it *harness.IT) {
	build := it.Path("src/app/BUILD.bazel")
	it.RequireContains(build, `types = ["vite/client"]`,
		"//src/app carries no types for a package-only list")
	it.RequireNotContains(build, "types_srcs",
		"//src/app got a types_srcs with no file to stage")
	it.Pass("//src/app carries the package-only list and no types_srcs")

	restore := it.Read(build)
	it.Replace(build, "    types = [\"vite/client\"],\n", "")
	log, err := it.BazelLog("package_entry_inherited_only", "build", "//src/app")
	it.Write(build, restore)
	if err == nil {
		log.Dump()
		it.Fail("//src/app compiled with the entry inherited through extends alone; Gazelle need not write it")
	}
	if !log.Contains("TS2339") {
		log.Dump()
		it.Fail("//src/app failed for some other reason than import.meta.env")
	}
	it.Pass("with the entry inherited alone, import.meta.env is TS2339: the attribute is what puts vite/client in the program")
}
