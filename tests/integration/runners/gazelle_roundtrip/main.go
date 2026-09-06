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
		dirs := []string{"src/lib", "src/app", "src/icons", "src/typed", "src/env", "worker", "worker/src", "worker/test", "worker/test/deep", "generated_worker/src", "aliased", "aliased/shared", "aliased/src", "jsx", "jsx/runtime", "jsx/view", "configured", "configured/test"}

		// Written here rather than shipped in the workspace: a BUILD file under
		// a --deleted_packages entry is a package of the OUTER workspace, and
		// glob_workspace_files would stop collecting the fixture below it --
		// the same boundary this case is about.
		it.Write(it.Path("src/i18n/BUILD.bazel"), codegenGlobDirective)
		it.Write(it.Path("src/locales/BUILD.bazel"), outDirCodegenPackage)
		it.Write(it.Path("generated_worker/BUILD.bazel"), generatedWorkerPackage)
		it.Write(it.Path("devserver/BUILD.bazel"), handWrittenDevServerPackage)
		it.Write(it.Path("packages/shared/BUILD.bazel"), memberPackage)
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
		memberAfterFirstRun := it.Read(it.Path("packages/shared/BUILD.bazel"))

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

		declarationEntryResolvesThroughItsOwner(it)
		referenceTypesBecomeADep(it)
		jsxRuntimeBecomesADep(it)
		generatedDeclarationNamesItsGenerator(it)
		aliasedImportsAreDeps(it)
		codegenGlobLoads(it)
		outDirContentsAreNotSources(it)
		assetDeclarationTypeApplies(it)
		anchoredExcludeHitsOnePath(it)
		jsSrcsAreCompiledAndDeclared(it)
		checkedInDeclarationTypesTheJavaScript(it)
		lintWithoutTheLinterIsRefused(it, gazelleLog)
		programsAreListedWithTheToolchainsTsgo(it, gazelleLog)
		handWrittenDevServerIsLeftAlone(it, devServerAfterFirstRun)
		memberSelfImportTakesTheHubLabel(it, memberAfterFirstRun)
		packageRootVitestConfigReachesTheTestBelow(it)
		// Last: it rewrites the tree the checks above read.
		declarationMovesToACodegen(it)
	})
}

// packages/shared is a member of the staged lockfile entered through an exports
// map; the boundary directive makes it the one target the hub's `target` names.
const memberPackage = "# gazelle:ts_package_boundary tsconfig\n"

// Only the hub's view links the member at node_modules/<name>, in the test's
// forest and its runtime tree alike, so a test importing the member by name
// needs the hub label beside :shared.
func memberSelfImportTakesTheHubLabel(it *harness.IT, afterFirstRun string) {
	build := it.Path("packages/shared/BUILD.bazel")
	if second := it.Read(build); second != afterFirstRun {
		fmt.Fprintf(os.Stderr, "--- packages/shared/BUILD.bazel pass 1 ---\n%s--- pass 2 ---\n%s", afterFirstRun, second)
		it.Fail("the member's BUILD file changed between two Gazelle runs")
	}
	it.Pass("packages/shared/BUILD.bazel is identical across both Gazelle runs")

	it.RequireContains(build, `name = "shared_test"`,
		"Gazelle wrote no ts_test for the member's test files")
	// The member's own target too, from the relative imports of the sources
	// under test: the first target to carry the member and its hub label at once.
	const deps = "    deps = [\n        \":shared\",\n        \"@npm//:shared\",\n        \"@npm//:vitest\",\n    ],\n"
	if text := it.Read(build); !strings.Contains(text, deps) {
		fmt.Fprint(os.Stderr, text)
		it.Fail("//packages/shared:shared_test does not carry the hub label beside the member's own target")
	}
	it.Pass("//packages/shared:shared_test depends on @npm//:shared beside :shared")

	for _, rel := range []string{"packages/shared/src/entry.test.js", "packages/shared/src/wire.test.js"} {
		it.RequireFile(it.Bin(rel),
			"%s was not written; the `bazel build //...` above did not compile the test program", rel)
	}
	it.Pass("the test program resolved `shared` and `shared/wire` through the hub view's link")

	// The measurement behind writing the hub label: the member's own target
	// alone puts nothing at node_modules/shared.
	restore := it.Read(build)
	it.Replace(build, "        \"@npm//:shared\",\n", "")
	log, err := it.BazelLog("self_import_without_the_hub", "build", "//packages/shared:_shared_test_compile")
	it.Write(build, restore)
	if err == nil {
		log.Dump()
		it.Fail("the test program compiled without the hub label; Gazelle need not write it")
	}
	for _, specifier := range []string{"shared", "shared/wire"} {
		if !log.Contains(fmt.Sprintf("TS2307: Cannot find module '%s'", specifier)) {
			log.Dump()
			it.Fail("without the hub label the compile did not fail on %q", specifier)
		}
	}
	it.Pass("without the hub label `shared` and `shared/wire` are TS2307: only the hub's view links the member into the forest")

	// The view's package.json is the member's with its exports map rewritten to
	// the emitted files, so node resolves both specifiers through the link.
	log, err = it.BazelLog("self_import_at_run_time", "test", "//packages/shared:shared_test")
	if err != nil {
		log.Dump()
		it.Fail("//packages/shared:shared_test failed: %v", err)
	}
	for _, stale := range []string{
		`Failed to resolve entry for package "shared"`,
		`Cannot find package 'shared/wire'`,
	} {
		if log.Contains(stale) {
			log.Dump()
			it.Fail("the runtime link still fails to resolve: %q", stale)
		}
	}
	it.Pass("//packages/shared:shared_test type-checks and runs: the runtime link resolves `shared` and `shared/wire` through the member's exports map")
}

// configured/ keeps its vitest.config.mts beside package.json and its test one
// package down; the config answers the test's `virtual:answer` import.
const packageRootVitestConfigFilegroup = `filegroup(
    name = "vitest_config",
    srcs = ["vitest.config.mts"],
    visibility = ["//visibility:public"],
)
`

func packageRootVitestConfigReachesTheTestBelow(it *harness.IT) {
	it.RequireContains(it.Path("configured/BUILD.bazel"), packageRootVitestConfigFilegroup,
		"the package root writes no filegroup over its vitest config")
	it.Pass("configured/BUILD.bazel carries the vitest_config filegroup")

	below := it.Path("configured/test/BUILD.bazel")
	const attr = "    config = \"//configured:vitest_config\",\n"
	it.RequireContains(below, attr, "//configured/test:test_test names no config")
	it.Pass("//configured/test:test_test names the package root's config by label")

	// `bazel test //...` above ran it under the config; without the attr nothing
	// resolves the import the plugin answers.
	restore := it.Read(below)
	it.Replace(below, attr, "")
	log, err := it.BazelLog("test_without_the_package_root_config", "test", "//configured/test:test_test")
	it.Write(below, restore)
	if err == nil {
		log.Dump()
		it.Fail("//configured/test:test_test passed without the config; Gazelle need not write it")
	}
	if !log.Contains("virtual:answer") {
		log.Dump()
		it.Fail("without the config the test did not fail on the import the plugin answers")
	}
	it.Pass("without the config //configured/test:test_test fails on `virtual:answer`: the setting reached the test through the label")
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
	it.RequireNotContains(owner, "ts_compile(",
		"the ts_compile whose only src was the deleted declaration survived")
	it.Pass("worker/BUILD.bazel keeps the ts_codegen and loses the ts_compile")

	for _, dir := range []string{"worker/src", "worker/test", "worker/test/deep"} {
		build := it.Path(dir, "BUILD.bazel")
		it.RequireNotContains(build, `"//worker"`,
			"//%s still depends on the withdrawn ts_compile", dir)
		it.RequireContains(build, `"//worker:worker_types"`,
			"//%s does not depend on the ts_codegen staging the declaration", dir)
		it.Pass("//%s depends on the ts_codegen where it depended on the declaration's owner", dir)
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
	binary := regexp.MustCompile(`listing programs with \S+/ts/toolchain/tsgo_resolved/(?:tsc|tsgo) \(runfiles\)`).FindString(gazelleLog.Text)
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
	it.Pass("generated_worker/BUILD.bazel keeps the ts_codegen")

	below := it.Path("generated_worker/src/BUILD.bazel")
	it.RequireContains(below, `deps = ["//generated_worker:worker_types"]`,
		"//generated_worker/src does not depend on the ts_codegen staging the declaration its tsconfig names")
	it.RequireNotContains(below, "types =",
		"//generated_worker/src restates the tsconfig's types entry")
	it.RequireNotContains(below, "types_srcs",
		"//generated_worker/src names the declaration through an attribute the rule does not have")
	it.Pass("//generated_worker/src depends on the ts_codegen and restates nothing")

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
	it.Pass("the import of @roundtrip/locales resolves through the tsconfig's paths to :tree")

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

// aliased/tsconfig.json maps #shared/* to ./shared/* and a test in each of its two
// subdirectories imports through it: the BUILD file carries the owner's dep, nothing else.
func aliasedImportsAreDeps(it *harness.IT) {
	for _, dir := range []string{"aliased", "aliased/shared", "aliased/src"} {
		it.RequireNotContains(it.Path(dir, "BUILD.bazel"), "path_alias",
			"//%s restates the tsconfig's alias through an attribute the rule does not have", dir)
	}
	it.Pass("no rule under //aliased carries an alias attribute")

	covered := it.Path("aliased/shared/BUILD.bazel")
	it.RequireContains(covered, `":shared"`,
		"//aliased/shared:shared_test does not depend on the target owning util.ts, which it imports through #shared/ and by relative path alike")
	it.Pass("//aliased/shared:shared_test depends on :shared")

	uncovered := it.Path("aliased/src/BUILD.bazel")
	if n := strings.Count(it.Read(uncovered), `"//aliased/shared"`); n != 2 {
		fmt.Fprint(os.Stderr, it.Read(uncovered))
		it.Fail("aliased/src holds a ts_compile and a ts_test, both importing through #shared/; //aliased/shared is on %d rules, want 2", n)
	}
	it.Pass("both rules in //aliased/src depend on //aliased/shared through the alias")

	tests := strings.Fields(it.BazelStdout("query", "kind(ts_test, //aliased/...)"))
	if len(tests) != 2 {
		it.Fail("expected the two generated ts_tests under //aliased, got %v", tests)
	}
	it.Pass("%s ran under `bazel test //...` above, so both test programs resolved #shared/util", strings.Join(tests, " and "))

	// The measurement behind writing the dep at all: the alias maps to a file
	// only the dep edge stages.
	restore := it.Read(uncovered)
	it.Replace(uncovered, "    deps = [\"//aliased/shared\"],\n", "")
	log, err := it.BazelLog("alias_without_the_dep", "build", "//aliased/src:src")
	it.Write(uncovered, restore)
	if err == nil {
		log.Dump()
		it.Fail("//aliased/src:src compiled without the dep; the alias alone reaches the file and Gazelle need not write one")
	}
	if !log.Contains("TS2307") || !log.Contains("'#shared/util'") {
		log.Dump()
		it.Fail("//aliased/src:src failed for some other reason than the unstaged alias target")
	}
	it.Pass("without the dep `#shared/util` is TS2307: the alias maps to a file nothing stages")
}

// tsc finds env.d.ts's `/// <reference types="node" />` in node_modules/@types; the
// sandbox has none, so index.ts's `process` is TS2591 until a dep carries the declarations.
func referenceTypesBecomeADep(it *harness.IT) {
	build := it.Path("src/env/BUILD.bazel")
	it.RequireContains(build, `deps = ["@npm//:types_node"]`,
		"//src/env does not carry the dep its /// <reference types=\"node\" /> names")
	it.RequireNotContains(build, "types =",
		"the directive was written as a types attribute rather than a dep")
	it.Pass("//src/env carries @npm//:types_node from the directive and no types attribute")

	it.RequireFile(it.Bin("src/env/index.js"),
		"//src/env did not compile, so @types/node's declarations never reached its program")
	it.Pass("//src/env type-checks a use of `process` against declarations nothing imports")

	// The measurement behind writing the dep at all.
	restore := it.Read(build)
	it.Replace(build, "    deps = [\"@npm//:types_node\"],\n", "")
	log, err := it.BazelLog("reference_types_without_the_dep", "build", "//src/env")
	it.Write(build, restore)
	if err == nil {
		log.Dump()
		it.Fail("//src/env compiled without the dep; the directive alone reaches the declarations and Gazelle need not write one")
	}
	if !log.Contains("TS2591") {
		log.Dump()
		it.Fail("//src/env failed for some other reason than the missing global")
	}
	it.Pass("without the dep `process` is TS2591: the directive resolves to nothing in the sandbox")
}

// jsx/tsconfig.json names @acme/jsx as the JSX runtime and `paths` sends it to
// runtime/; view/Icon.tsx imports nothing, so the implicit import is the whole dep.
func jsxRuntimeBecomesADep(it *harness.IT) {
	build := it.Path("jsx/view/BUILD.bazel")
	it.RequireContains(build, `deps = ["//jsx/runtime"]`,
		"//jsx/view does not carry the runtime its tsconfig's jsxImportSource names")
	it.Pass("//jsx/view carries //jsx/runtime from jsxImportSource alone: nothing in it imports")

	it.RequireFile(it.Bin("jsx/view/Icon.js"),
		"//jsx/view did not compile, so @acme/jsx/jsx-runtime never reached its program")
	it.Pass("//jsx/view type-checks a JSX tag against a runtime nothing imports")

	// The measurement behind writing the dep at all.
	restore := it.Read(build)
	it.Replace(build, "    deps = [\"//jsx/runtime\"],\n", "")
	log, err := it.BazelLog("jsx_runtime_without_the_dep", "build", "//jsx/view")
	it.Write(build, restore)
	if err == nil {
		log.Dump()
		it.Fail("//jsx/view compiled without the dep; the tsconfig alone reaches the runtime and Gazelle need not write one")
	}
	if !log.Contains("TS2875") || !log.Contains("'@acme/jsx/jsx-runtime'") {
		log.Dump()
		it.Fail("//jsx/view failed for some other reason than the missing runtime")
	}
	it.Pass("without the dep the tag is TS2875 on '@acme/jsx/jsx-runtime': the implicit import resolves to nothing in the sandbox")
}

// worker/tsconfig.json states `types: ["./worker-configuration.d.ts"]` and handler.ts
// uses its declarations: the BUILD file carries the dep on the target holding the file.
func declarationEntryResolvesThroughItsOwner(it *harness.IT) {
	owner := it.Path("worker/BUILD.bazel")
	it.RequireContains(owner, `srcs = ["worker-configuration.d.ts"]`,
		"the tsconfig's own package holds no target for the declaration it names")
	it.Pass("//worker holds worker-configuration.d.ts")

	below := it.Path("worker/src/BUILD.bazel")
	for _, attr := range []string{"types =", "types_srcs", "tsconfig_types"} {
		it.RequireNotContains(below, attr,
			"//worker/src restates the tsconfig's entry as %q", attr)
	}
	if n := strings.Count(it.Read(below), `"//worker"`); n != 2 {
		fmt.Fprint(os.Stderr, it.Read(below))
		it.Fail("worker/src holds a ts_compile and a ts_test; //worker is on %d rules, want 2", n)
	}
	it.Pass("both rules in //worker/src depend on //worker and restate nothing")

	it.RequireFile(it.Bin("worker/src/handler.js"),
		"//worker/src did not compile, so the declarations never reached its program")
	it.Pass("//worker/src type-checks against declarations nothing there imports")

	declarationWithoutItsOwnerIsNotStaged(it, below)
	theDeclarationStopsAtTheSubtree(it)
	parentEntryResolvesThroughTheOwner(it)
	packageEntryIsADep(it)
}

// The measurement behind writing the dep at all: the entry names a file only the
// dep edge stages, and the rule refuses an entry nothing stages before tsgo runs.
func declarationWithoutItsOwnerIsNotStaged(it *harness.IT, below string) {
	restore := it.Read(below)
	it.Write(below, `load("@rules_typescript//ts:defs.bzl", "ts_compile")

ts_compile(
    name = "src",
    srcs = ["handler.ts"],
    tsconfig = "//worker:tsconfig",
    visibility = ["//visibility:public"],
)
`)
	log, err := it.BazelLog("types_entry_without_the_owner", "build", "//worker/src")
	it.Write(below, restore)
	if err == nil {
		log.Dump()
		it.Fail("//worker/src compiled without the dep; the entry alone reaches the declaration and Gazelle need not write one")
	}
	if !log.Contains("worker/worker-configuration.d.ts, which no input of this action sits at") {
		log.Dump()
		it.Fail("//worker/src failed for some other reason than the entry naming a file nothing stages")
	}
	it.Pass("without the dep the entry names a file no input sits at: the dep edge is what stages the declaration")
}

// A dep edge stages a file and puts nothing in scope: a consumer outside the
// tsconfig's subtree that deps a target inside it gets no declaration.
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

// worker/test names the declaration as `../` and worker/test/deep as `../../`;
// each program's own tsconfig sets the entry, and the dep is the same owner.
func parentEntryResolvesThroughTheOwner(it *harness.IT) {
	for _, dir := range []string{"worker/test", "worker/test/deep"} {
		build := it.Path(dir, "BUILD.bazel")
		for _, attr := range []string{"types =", "types_srcs", "tsconfig_types"} {
			it.RequireNotContains(build, attr, "//%s restates the tsconfig's entry as %q", dir, attr)
		}
		it.RequireContains(build, `"//worker"`,
			"//%s does not depend on the target owning the declaration its tsconfig names", dir)
		it.Pass("//%s depends on //worker and restates nothing", dir)
	}

	targets := strings.Fields(it.BazelStdout("query", "kind(ts_test, //worker/test/...)"))
	if len(targets) != 2 {
		it.Fail("expected one generated ts_test in each of //worker/test and //worker/test/deep, got %v", targets)
	}
	it.Pass("%s ran under `bazel test //...` above, so each test program resolved its entry through //worker", strings.Join(targets, " and "))
}

// src/app/tsconfig.json names vite/client and no file; the entry resolves through
// the forest, so what the BUILD file carries is the dep that puts vite in it.
func packageEntryIsADep(it *harness.IT) {
	build := it.Path("src/app/BUILD.bazel")
	it.RequireNotContains(build, "types =",
		"//src/app restates the tsconfig's package-only types list")
	it.RequireContains(build, `"@npm//:vite"`,
		"//src/app does not depend on the package its types entry names")
	it.Pass("//src/app depends on @npm//:vite for vite/client and restates nothing")

	restore := it.Read(build)
	it.Replace(build, "        \"@npm//:vite\",\n", "")
	log, err := it.BazelLog("package_entry_without_the_dep", "build", "//src/app")
	it.Write(build, restore)
	if err == nil {
		log.Dump()
		it.Fail("//src/app compiled without the dep; the entry alone reaches vite/client and Gazelle need not write one")
	}
	if !log.Matches(`(?i)TS2688.*vite/client|TS2339`) {
		log.Dump()
		it.Fail("//src/app failed for some other reason than vite/client resolving to nothing")
	}
	it.Pass("without the dep vite/client resolves to nothing in the forest: the dep is what puts it in the program")
}
