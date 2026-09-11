package main

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
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

// labels is what Bazel loaded for one attribute of one target, sorted.
func labels(it *harness.IT, attr, target string) []string {
	query := fmt.Sprintf("labels(%s, %s)", attr, target)
	out := strings.Fields(it.BazelStdout("query", query))
	sort.Strings(out)
	return out
}

func requireLabels(it *harness.IT, attr, target string, want []string) {
	if got := labels(it, attr, target); !reflect.DeepEqual(got, want) {
		it.Fail("%s of %s = %v, want %v", attr, target, got, want)
	}
}

// The ts_test Gazelle writes for a package is named after its directory.
func testTarget(dir string) string {
	return fmt.Sprintf("//%s:%s_test", dir, filepath.Base(dir))
}

// A ts_test's deps carry its manifest's dependencies and devDependencies
// beside its edges; under the root package.json that is these six.
var rootManifestDeps = []string{"//:node_modules/shared", "@npm//:culori",
	"@npm//:types_culori", "@npm//:types_node", "@npm//:vite", "@npm//:vitest"}

func withRootManifest(own ...string) []string {
	return slices.Sorted(slices.Values(slices.Concat(own, rootManifestDeps)))
}

// The packages Gazelle writes whole: deleted between the two passes.
var generated = []string{
	"src/lib", "src/app", "src/icons", "src/typed", "src/env",
	"worker", "worker/test", "worker/test/deep",
	"aliased", "aliased/src", "jsx", "jsx/runtime",
	"configured", "configured/test", "packages/shared",
	"member", "dotdot", "dotdot/inner", "pooled", "pooled/test",
}

// BUILD files a run merges into rather than writes, the root's among them.
var handWritten = []string{
	"", "src/i18n", "src/locales", "generated_worker", "devserver",
	"shared_program", "wrangler_lock",
}

func main() {
	harness.Run(harness.Config{
		Name:         "gazelle_roundtrip",
		WorkspaceRel: "tests/integration/gazelle_roundtrip",
	}, func(it *harness.IT) {
		// Written here, not shipped: a BUILD file under a --deleted_packages
		// entry is the OUTER workspace's, and glob_workspace_files stops there.
		it.Write(it.Path("src/i18n/BUILD.bazel"), cataloguePackage)
		it.Write(it.Path("src/locales/BUILD.bazel"), outDirCodegenPackage)
		it.Write(it.Path("generated_worker/BUILD.bazel"), generatedWorkerPackage)
		it.Write(it.Path("devserver/BUILD.bazel"), handWrittenDevServerPackage)
		it.Write(it.Path("shared_program/BUILD.bazel"), sharedProgramPackage)
		it.Write(it.Path("worker/src/BUILD.bazel"), staleWorkerSrcPackage)
		it.Write(it.Path("wrangler_lock/BUILD.bazel"), wranglerLock)
		// wrangler, for generated_worker's ts_codegen, is in tests/workers' lockfile.
		it.Write(it.Path("wrangler_lock/pnpm-lock.yaml"),
			it.Read(filepath.Join(it.RulesTSRoot, "tests/workers/pnpm-lock.yaml")))

		it.Install()
		it.Pass("pnpm install: the listing runs over the tree the build will check")

		gazelleLog, err := it.BazelLog("gazelle_pass_1", "run", "//:gazelle", "--", "-ts_verbose")
		if err != nil {
			gazelleLog.Dump()
			it.Fail("bazel run //:gazelle exited non-zero: %v", err)
		}
		it.Pass("gazelle pass 1 complete")

		for _, dir := range generated {
			it.RequireFile(it.Path(dir, "BUILD.bazel"), "Gazelle did not generate %s/BUILD.bazel", dir)
			it.Pass("%s/BUILD.bazel generated (pass 1)", dir)
		}
		afterFirstRun := map[string]string{}
		for _, dir := range handWritten {
			afterFirstRun[dir] = it.Read(it.Path(dir, "BUILD.bazel"))
		}
		staleBuildFileIsEmptiedAndNamed(it, gazelleLog)

		before := testTargets(it)
		if len(before) == 0 {
			it.Fail("no test targets in this workspace: the set comparison below would be vacuous")
		}
		it.Pass("test targets after pass 1: %d", len(before))

		firstPass := map[string]string{}
		for _, dir := range generated {
			firstPass[dir] = it.Read(it.Path(dir, "BUILD.bazel"))
			if err := os.Remove(it.Path(dir, "BUILD.bazel")); err != nil {
				it.Fail("cannot delete %s/BUILD.bazel: %v", dir, err)
			}
		}
		it.Pass("BUILD files snapshotted and deleted")

		it.MustBazel("run", "//:gazelle")
		it.Pass("gazelle pass 2 complete")

		differs := false
		for _, dir := range generated {
			second := it.Read(it.Path(dir, "BUILD.bazel"))
			if second != firstPass[dir] {
				fmt.Fprintf(os.Stderr, "--- %s/BUILD.bazel pass 1 ---\n%s--- pass 2 ---\n%s", dir, firstPass[dir], second)
				differs = true
				continue
			}
			it.Pass("%s/BUILD.bazel is identical across both Gazelle runs", dir)
		}
		for _, dir := range handWritten {
			second := it.Read(it.Path(dir, "BUILD.bazel"))
			if second != afterFirstRun[dir] {
				fmt.Fprintf(os.Stderr, "--- %s/BUILD.bazel pass 1 ---\n%s"+
					"--- pass 2 ---\n%s", dir, afterFirstRun[dir], second)
				differs = true
				continue
			}
			it.Pass("%s/BUILD.bazel, merged into, is identical across both Gazelle runs",
				filepath.Join(".", dir))
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

		theRootBaseIsATsConfig(it)
		declarationEntryResolvesThroughItsOwner(it)
		referenceTypesBecomeADep(it)
		jsxRuntimeBecomesADep(it)
		generatedDeclarationNamesItsGenerator(it)
		aliasedImportsAreDeps(it)
		theTreesFilesAreSrcs(it)
		outDirContentsAreNotSources(it)
		assetImportTypesThroughItsDeclaration(it)
		theTsconfigsExcludeIsTheExclusion(it)
		allowJsCompilesAndDeclares(it)
		checkedInDeclarationTypesTheJavaScript(it)
		programsAreListedWithTheToolchainsTsgo(it, gazelleLog)
		handWrittenDevServerIsLeftAlone(it)
		handWrittenRuleSharesTheProgram(it)
		memberSelfImportIsTheCompile(it)
		memberByNameIsTheHubView(it)
		parentDirectoryImportIsADep(it)
		packageRootVitestConfigReachesTheTestBelow(it)
		configBesideTheTestsIsNamed(it)
		workersPoolConfigReachesTheTest(it)
		importerScopedLabelsResolve(it)
		pairedTypesReachTheProgramThroughTheChain(it)
		// Last: it rewrites the tree the checks above read.
		declarationMovesToACodegen(it)
	})
}

const loadTsCompile = `load("@rules_typescript//ts:defs.bzl", "ts_compile")
`

const loadTsCodegen = `load("@rules_typescript//ts:defs.bzl", "ts_codegen")
`

// What an every-directory run left below worker/: worker/src holds no
// tsconfig.json, so the package model empties the file and names it.
const staleWorkerSrcPackage = loadTsCompile + `
ts_compile(
    name = "src",
    srcs = ["handler.ts"],
    tsconfig = "//worker:tsconfig",
)
`

func staleBuildFileIsEmptiedAndNamed(it *harness.IT, gazelleLog *harness.Log) {
	stale := it.Path("worker/src/BUILD.bazel")
	it.RequireNotContains(stale, "ts_compile(",
		"the stale ts_compile below //worker survived the run")
	it.Pass("worker/src/BUILD.bazel holds no rule after the run: " +
		"worker/src is no package")
	for _, want := range []string{
		"worker/src is not a package", "ts_compile(src) is withdrawn",
	} {
		if !gazelleLog.Contains(want) {
			gazelleLog.Dump()
			it.Fail("Gazelle did not say %q of the emptied BUILD file", want)
		}
	}
	it.Pass("Gazelle named worker/src/BUILD.bazel as the file to delete")

	// Empty, the file still makes worker/src a package holding //worker's
	// src/handler.ts, so the consumer deletes it before the build.
	if err := os.Remove(stale); err != nil {
		it.Fail("cannot delete the emptied BUILD file: %v", err)
	}
	it.Pass("worker/src/BUILD.bazel deleted, as a consumer deletes what " +
		"Gazelle empties")
}

// The root tsconfig.json every package extends has no include: no program,
// and a ts_config because the chains name it.
func theRootBaseIsATsConfig(it *harness.IT) {
	root := it.Path("BUILD.bazel")
	it.RequireContains(root, `name = "tsconfig"`,
		"no ts_config was written for the root tsconfig.json the packages extend")
	it.RequireNotContains(root, `name = "root"`,
		"Gazelle wrote a compile target for the root, whose tsconfig.json "+
			"enumerates nothing")
	it.Pass("the root gets a ts_config and no compile target")
	requireLabels(it, "deps", "//src/lib:tsconfig", []string{"//:tsconfig"})
	it.Pass("//src/lib:tsconfig depends on //:tsconfig, the base its " +
		"extends names")
}

// The member's own tests import it by name: a self-reference through the
// manifest as built at the member's path, the compile alone their dep.
func memberSelfImportIsTheCompile(it *harness.IT) {
	build := it.Path("packages/shared/BUILD.bazel")
	it.RequireContains(build, `name = "shared_test"`,
		"Gazelle wrote no ts_test for the member's test files")
	requireLabels(it, "deps", "//packages/shared:shared_test",
		[]string{"//packages/shared:shared", "@npm//:vitest"})
	it.Pass("//packages/shared:shared_test depends on :shared and on no " +
		"link target: the member's own name is a self-reference")

	for _, rel := range []string{"packages/shared/src/entry.test.js", "packages/shared/src/wire.test.js"} {
		it.RequireFile(it.Bin(rel),
			"%s was not written; the `bazel build //...` above did not compile the test program", rel)
	}
	it.Pass("the test program resolved `shared` and `shared/wire` through " +
		"the manifest as built laid at packages/shared/package.json")

	log, err := it.BazelLog("self_import_at_run_time", "test",
		"//packages/shared:shared_test")
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
			it.Fail("the runtime still fails to resolve: %q", stale)
		}
	}
	it.Pass("//packages/shared:shared_test type-checks and runs: node resolves " +
		"`shared` and `shared/wire` through the member's exports map at its " +
		"own path")
}

// member/'s test imports the member by name and by its exports subpath from
// another package: the dep is the root's link target, never the ts_compile.
func memberByNameIsTheHubView(it *harness.IT) {
	requireLabels(it, "deps", "//member:member_test", withRootManifest())
	it.Pass("//member:member_test depends on //:node_modules/shared, the root's " +
		"link target, for `shared` and `shared/wire`")
	it.RequireFile(it.Bin("member/consumer.test.js"),
		"the member's consumer did not compile against the view")
	it.Pass("the test compiled and ran under `bazel test //...` above, " +
		"through the importer's link")
}

// dotdot/inner imports "..", the package above: the listing resolves the
// specifier to dotdot/index.ts and ownership names //dotdot.
func parentDirectoryImportIsADep(it *harness.IT) {
	requireLabels(it, "deps", "//dotdot/inner:inner", []string{"//dotdot:dotdot"})
	it.RequireFile(it.Bin("dotdot/inner/leaf.js"),
		"//dotdot/inner did not compile")
	it.Pass("//dotdot/inner depends on //dotdot through an import of `..`")
}

// configured/ keeps its vitest.config.mts beside package.json and its test one
// package down; the config answers the test's `virtual:answer` import.
func packageRootVitestConfigReachesTheTestBelow(it *harness.IT) {
	requireLabels(it, "srcs", "//configured:vitest_config",
		[]string{"//configured:vitest.config.mts"})
	it.Pass("//configured:vitest_config holds the package root's vitest config")

	below := it.Path("configured/test/BUILD.bazel")
	const attr = "    config = \"//configured:vitest_config\",\n"
	requireLabels(it, "config", "//configured/test:test_test",
		[]string{"//configured:vitest_config"})
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

// worker/test keeps a vitest.config.mts beside its tests; its tsconfig lists
// *.ts alone, so the config is the ts_test's config and no target's src.
func configBesideTheTestsIsNamed(it *harness.IT) {
	requireLabels(it, "config", "//worker/test:test_test",
		[]string{"//worker/test:vitest.config.mts"})
	for _, src := range labels(it, "srcs", "//worker/test:test_test") {
		if strings.HasSuffix(src, "vitest.config.mts") {
			it.Fail("the vitest config is a src of the test that runs under it: %s", src)
		}
	}
	it.Pass("//worker/test:test_test names vitest.config.mts as its config " +
		"and compiles it in no target")
}

// pooled/vitest.config.mts installs the Workers pool and names its wrangler
// config: the worker makes the file a label, the test names it, workerd runs.
func workersPoolConfigReachesTheTest(it *harness.IT) {
	requireLabels(it, "srcs", "//pooled:wrangler_config",
		[]string{"//pooled:wrangler.jsonc"})
	requireLabels(it, "wrangler_config", "//pooled/test:test_test",
		[]string{"//pooled:wrangler_config"})
	it.Pass("//pooled/test:test_test names //pooled:wrangler_config, the " +
		"filegroup over the file the config's configPath names")

	below := it.Path("pooled/test/BUILD.bazel")
	it.RequireContains(below, `coverage_provider = "istanbul"`,
		"the pooled test got no coverage_provider; the pool refuses v8")
	requireLabels(it, "deps", "//pooled/test:test_test", []string{
		"//pooled:pooled", "@npm//pooled:cloudflare_vitest-pool-workers",
		"@npm//pooled:cloudflare_workers-types", "@npm//pooled:vitest",
		"@npm//pooled:vitest_coverage-istanbul"})
	it.Pass("coverage_provider is istanbul and @vitest/coverage-istanbul is a " +
		"dep in the importer's spelling")

	// `bazel test //...` above ran it in workerd; the measurement behind the
	// attribute: without it the pool boots the source `main`.
	const attr = "    wrangler_config = \"//pooled:wrangler_config\",\n"
	restore := it.Read(below)
	it.Replace(below, attr, "")
	log, err := it.BazelLog("pooled_without_wrangler_config", "test",
		"//pooled/test:test_test")
	it.Write(below, restore)
	if err == nil {
		log.Dump()
		it.Fail("//pooled/test:test_test passed without wrangler_config; " +
			"Gazelle need not write it")
	}
	if !log.Contains("src/index.ts") {
		log.Dump()
		it.Fail("without wrangler_config the test did not fail on the source main")
	}
	it.Pass("without wrangler_config the pool boots src/index.ts, which the " +
		"runfiles do not hold: the patched copy reached the test through the label")
}

// configured/package.json declares vitest and is a lockfile importer, so its
// test's label names that importer's resolution (D7).
func importerScopedLabelsResolve(it *harness.IT) {
	requireLabels(it, "deps", "//configured/test:test_test",
		[]string{"@npm//configured:vitest"})
	it.Pass("//configured/test:test_test depends on @npm//configured:vitest, " +
		"the importer's own resolution, and it ran above")
}

// src/app imports culori, which ships no declarations: the edge's label is the
// package the specifier names, and @types/culori arrives paired on the chain.
func pairedTypesReachTheProgramThroughTheChain(it *harness.IT) {
	requireLabels(it, "deps", "//src/app:app",
		[]string{"//src/lib:lib", "@npm//:culori", "@npm//:vite"})
	it.Pass("//src/app depends on @npm//:culori alone and type-checked " +
		"formatHex through the paired @types/culori")
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
func handWrittenDevServerIsLeftAlone(it *harness.IT) {
	build := it.Path("devserver/BUILD.bazel")
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

// The node_tooling shape: a kept hand-written rule globs tools/, while the
// program lists tools/helper.ts by import, so the glob leaves that file out.
const sharedProgramPackage = loadTsCompile + `
# keep
ts_compile(
    name = "tooling",
    srcs = glob(
        ["tools/**/*.ts"],
        exclude = ["tools/helper.ts"],
    ),
    tsconfig = ":tsconfig",
    deps = [":shared_program"],
)
`

func handWrittenRuleSharesTheProgram(it *harness.IT) {
	build := it.Path("shared_program/BUILD.bazel")
	it.RequireContains(build, `name = "tooling"`,
		"the kept rule did not survive the Gazelle runs")
	it.RequireContains(build, `name = "shared_program"`,
		"Gazelle wrote no ts_compile for the program beside the kept rule")
	it.Pass("shared_program/BUILD.bazel holds the kept rule and the generated one")

	requireLabels(it, "srcs", "//shared_program:shared_program",
		[]string{"//shared_program:src/main.ts", "//shared_program:tools/helper.ts"})
	it.Pass("//shared_program compiles tools/helper.ts, which its program " +
		"lists by import")
	requireLabels(it, "srcs", "//shared_program:tooling",
		[]string{"//shared_program:tools/cli.ts"})
	it.Pass("//shared_program:tooling globs the rest of tools/ and excludes " +
		"the program's file")
	for _, rel := range []string{
		"shared_program/tools/helper.js", "shared_program/tools/cli.js",
	} {
		it.RequireFile(it.Bin(rel), "expected output file not found: %s", rel)
	}
	it.Pass("both targets built: cli.ts resolves ./helper through the dep " +
		"on the program's target")
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

// The copy on disk stays, in a shape no program here compiles against: the
// declared out is the codegen's, and a green build says which copy was staged.
func declarationMovesToACodegen(it *harness.IT) {
	it.Write(it.Path("worker/worker-configuration.d.ts"),
		"declare const WORKER_BUILD_ID: string;\n\n"+
			"interface WorkerEnv {\n\treadonly stale: string;\n}\n")
	it.Write(it.Path("worker/bindings.txt"), "bucket\n")
	owner := it.Path("worker/BUILD.bazel")
	it.Write(owner, it.Read(owner)+workerTypesCodegen)
	it.MustBazel("run", "//:gazelle")
	it.Pass("gazelle run over the migration: ts_codegen appended, a stale " +
		"copy left on disk")

	it.RequireContains(owner, `name = "worker_types"`,
		"the hand-written ts_codegen did not survive the Gazelle run")
	requireLabels(it, "srcs", "//worker:worker",
		[]string{"//worker:bindings.txt", "//worker:src/handler.ts"})
	requireLabels(it, "srcs", "//worker:worker_test",
		[]string{"//worker:src/handler.test.ts"})
	requireLabels(it, "deps", "//worker:worker", []string{"//worker:worker_types"})
	it.Pass("worker/BUILD.bazel keeps the ts_codegen; no rule in //worker " +
		"lists the declared out on disk, and //worker gains the codegen")

	for _, dir := range []string{"worker/test", "worker/test/deep"} {
		requireLabels(it, "deps", testTarget(dir),
			withRootManifest("//worker:worker", "//worker:worker_types"))
		it.Pass("//%s depends on the ts_codegen for the entry and on //worker "+
			"for the import", dir)
	}

	dirs := []string{"worker", "worker/test", "worker/test/deep"}
	written := map[string]string{}
	for _, dir := range dirs {
		written[dir] = it.Read(it.Path(dir, "BUILD.bazel"))
	}
	it.MustBazel("run", "//:gazelle")
	for _, dir := range dirs {
		again := it.Read(it.Path(dir, "BUILD.bazel"))
		if again != written[dir] {
			fmt.Fprintf(os.Stderr, "--- %s/BUILD.bazel run 1 ---\n%s"+
				"--- run 2 ---\n%s", dir, written[dir], again)
			it.Fail("a second Gazelle run rewrote %s/BUILD.bazel", dir)
		}
	}
	it.Pass("a second Gazelle run writes nothing under worker/")

	it.MustBazel("test", "//worker/...")
	it.Pass("bazel test //worker/...: every program under worker/ resolves " +
		"the generated declaration")
	declaration := it.Bin("worker/worker-configuration.d.ts")
	it.RequireContains(declaration, "readonly bucket: string;",
		"the generated declaration names no binding from bindings.txt")
	it.Pass("the programs compiled against the generated declaration: each " +
		"reads env.bucket, which the copy on disk does not declare")
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

	for _, dir := range []string{
		"src/app", "src/locales", "aliased", "worker", "worker/test",
		"worker/test/deep", "generated_worker", "packages/shared", "member",
		"dotdot/inner",
	} {
		if !gazelleLog.Matches(dir + `/tsconfig.json: \d+ files listed, [1-9]\d* roots, \d+ edges, \d+ type entries`) {
			gazelleLog.Dump()
			it.Fail("Gazelle did not list %s/tsconfig.json", dir)
		}
		it.Pass("%s/tsconfig.json listed", dir)
	}
	if !gazelleLog.Contains("tsconfig.json: not listed: neither include nor " +
		"files in its extends chain") {
		gazelleLog.Dump()
		it.Fail("Gazelle listed the root tsconfig.json, which would enumerate " +
			"the whole workspace")
	}
	it.Pass("the root tsconfig.json is refused before tsgo runs")
	summary := regexp.MustCompile(`\d+ \.ts/\.tsx/\.mts/\.cts files in no program across \d+ director(y|ies)`).FindString(gazelleLog.Text)
	if summary == "" {
		gazelleLog.Dump()
		it.Fail("Gazelle did not report the files in no program once the walk was done")
	}
	it.Pass("Gazelle reports the files in no program: %s", summary)
}

// A ts_codegen is hand-written; its srcs reach into messages/, which no
// BUILD file makes a package.
const cataloguePackage = loadTsCodegen + `
ts_codegen(
    name = "locales",
    srcs = ["settings.json"] + glob(["messages/*.json"]),
    outs = ["locales.ts"],
    args = [
        "{srcs}",
        "{out}",
    ],
    generator = "//:catalogue_gen",
)
`

// src/i18n's program lists settings.json by import; the tree's other JSON is
// the ts_compile's by the walk, and the codegen beside it is its dep.
func theTreesFilesAreSrcs(it *harness.IT) {
	it.RequireNoFile(it.Path("src/i18n/messages/BUILD.bazel"),
		"Gazelle made src/i18n/messages a package; no tsconfig.json is there")
	it.Pass("src/i18n/messages has no BUILD file of its own")

	requireLabels(it, "srcs", "//src/i18n:i18n", []string{
		"//src/i18n:index.ts", "//src/i18n:messages/en.json",
		"//src/i18n:messages/sv.json", "//src/i18n:settings.json",
	})
	it.Pass("//src/i18n holds the imported settings.json and the messages " +
		"the walk found")
	requireLabels(it, "deps", "//src/i18n:i18n", []string{"//src/i18n:locales"})
	it.RequireFile(it.Bin("src/i18n/index.js"),
		"//src/i18n did not compile; the JSON import did not resolve in the sandbox")
	it.Pass("//src/i18n depends on :locales and type-checks the import of " +
		"settings.json")

	generated := it.Bin("src/i18n/locales.ts")
	it.RequireFile(generated, "the codegen action did not run")
	for _, name := range []string{"settings.json", "en.json", "sv.json"} {
		it.RequireContains(generated, name, "the codegen action never saw %s", name)
		it.Pass("the codegen action saw %s", name)
	}
}

// A hand-written out_dir ts_codegen, with a tree checked in under compiled/
// standing in for what a local run leaves behind.
const outDirCodegenPackage = loadTsCodegen + `
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

// The workers lockfile's package holds its store and nothing else.
const wranglerLock = `load("@npm_workers//:defs.bzl", "npm_virtual_store")

npm_virtual_store(name = "node_modules/.pnpm")
`

// A worker with nothing checked in: the ts_codegen beside the config writes the
// declaration its tsconfig names; its node_modules links another lockfile.
const generatedWorkerPackage = `load(
    "@rules_typescript//npm:defs.bzl",
    "node_modules",
)
load("@rules_typescript//ts:defs.bzl", "ts_codegen")

# keep
node_modules(
    name = "node_modules",
    hoist = "//wrangler_lock:node_modules/.pnpm/node_modules",
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
	build := it.Path("generated_worker/BUILD.bazel")
	it.RequireContains(build, `name = "worker_types"`,
		"the hand-written ts_codegen did not survive the Gazelle run")
	requireLabels(it, "deps", "//generated_worker:generated_worker",
		[]string{"//generated_worker:worker_types"})
	for _, attr := range []string{"types =", "types_srcs"} {
		it.RequireNotContains(build, attr,
			"//generated_worker restates the tsconfig's types entry as %q", attr)
	}
	it.Pass("//generated_worker depends on the ts_codegen and restates nothing")

	declaration := it.Bin("generated_worker/worker-configuration.d.ts")
	it.RequireFile(declaration, "the wrangler types action did not run")
	it.RequireContains(declaration, "CACHE: KVNamespace;",
		"the generated declaration names no binding from wrangler.jsonc")
	it.Pass("wrangler types wrote the declaration from wrangler.jsonc, with no checked-in copy")
	it.RequireFile(it.Bin("generated_worker/src/index.js"),
		"//generated_worker did not compile against the generated declaration")
	it.Pass("//generated_worker type-checks against the Env and runtime " +
		"globals the generated file declares")

	// A binding the config does not declare; only the generated Env can refuse it.
	consumer := it.Path("generated_worker/src/index.ts")
	restore := it.Read(consumer)
	it.Write(consumer, strings.Replace(restore, "env.GREETING", "env.MISSING", 1))
	log, err := it.BazelLog("generated_env_is_in_force", "build",
		"//generated_worker")
	it.Write(consumer, restore)
	if err == nil {
		log.Dump()
		it.Fail("//generated_worker compiled a binding wrangler.jsonc does " +
			"not declare; the generated Env is not the type in force")
	}
	if !log.Contains("TS2339") {
		log.Dump()
		it.Fail("//generated_worker failed for some other reason than the " +
			"undeclared binding")
	}
	it.Pass("env.MISSING is TS2339, so the generated Env types the worker")
}

// A file under the out_dir read into srcs is one output declared twice, the tree
// and a file inside it; the `bazel build //...` above is where Bazel says so.
func outDirContentsAreNotSources(it *harness.IT) {
	requireLabels(it, "srcs", "//src/locales:locales",
		[]string{"//src/locales:app.ts", "//src/locales:names.txt"})
	it.Pass("//src/locales holds the tree's files and nothing under compiled/")

	requireLabels(it, "deps", "//src/locales:locales",
		[]string{"//src/locales:tree"})
	it.Pass("the import of @roundtrip/locales resolves through the tsconfig's paths to :tree")

	for _, dir := range []string{"src/locales/compiled", "src/locales/compiled/messages"} {
		it.RequireNoFile(it.Path(dir, "BUILD.bazel"), "Gazelle made %s a package inside :tree's out_dir", dir)
	}
	it.Pass("no package under src/locales/compiled")

	it.RequireFile(it.Bin("src/locales/app.js"), "//src/locales did not compile against the tree")
	it.Pass("//src/locales compiles against the generated tree")
}

// src/lib/tsconfig.json excludes lib.config.ts; src/app's namesake is in its
// own program, so the exclusion is the tsconfig's and reaches one file.
func theTsconfigsExcludeIsTheExclusion(it *harness.IT) {
	requireLabels(it, "srcs", "//src/lib:lib", []string{
		"//src/lib:format.mjs", "//src/lib:index.ts", "//src/lib:math.ts"})
	it.Pass("//src/lib holds format.mjs under allowJs and no lib.config.ts")

	requireLabels(it, "srcs", "//src/app:app",
		[]string{"//src/app:index.ts", "//src/app:lib.config.ts"})
	it.Pass("//src/app still holds its own lib.config.ts")

	it.RequireFile(it.Bin("src/app/lib.config.js"),
		"src/app/lib.config.ts is in the srcs but nothing compiled it")
	it.Pass("src/app/lib.config.ts compiles; src/lib's namesake is out of the build")
}

// logo.svg is a src by the walk and assets.d.ts's module declaration types the
// import; only a compile that has to fail says the declaration is in force.
func assetImportTypesThroughItsDeclaration(it *harness.IT) {
	requireLabels(it, "srcs", "//src/icons:icons",
		[]string{"//src/icons:assets.d.ts", "//src/icons:index.ts",
			"//src/icons:logo.svg"})
	it.Pass("//src/icons holds logo.svg beside the declaration that types it")

	// //src/icons compiled above, reading logo.viewBox: TS2339 on a string.
	consumer := it.Path("src/icons/index.ts")
	restore := it.Read(consumer)
	it.Write(consumer, "import logo from \"./logo.svg\";\n\nexport const url: string = logo;\n")
	log, err := it.BazelLog("asset_declaration_is_not_a_string",
		"build", "//src/icons")
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

// src/lib/tsconfig.json sets allowJs, so tsgo lists format.mjs and the rule
// emits its .d.mts; a consumer's failing compile says which type is in force.
func allowJsCompilesAndDeclares(it *harness.IT) {
	for _, rel := range []string{"src/lib/format.mjs", "src/lib/format.d.mts"} {
		it.RequireFile(it.Bin(rel), "expected output file not found: %s", rel)
		it.Pass("output file exists: %s", rel)
	}

	// //src/app compiled above against ../lib, which re-exports format; without
	// the .d.mts the import widens to `any` and any use of it compiles.
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

// The monorepo shape: an untyped compile.mjs, the compile.d.mts tsc reads in
// its place, and a test importing "./compile.mjs"; a failing compile tells.
func checkedInDeclarationTypesTheJavaScript(it *harness.IT) {
	requireLabels(it, "srcs", "//src/typed:typed",
		[]string{"//src/typed:compile.d.mts", "//src/typed:compile.mjs"})
	it.Pass("//src/typed holds the declaration and the JavaScript it stands " +
		"for, which tsgo does not list")
	requireLabels(it, "srcs", "//src/typed:typed_test",
		[]string{"//src/typed:compile.d.mts", "//src/typed:compile.test.ts"})
	requireLabels(it, "deps", "//src/typed:typed_test",
		withRootManifest("//src/typed:typed"))
	it.Pass("//src/typed:typed_test carries the declaration and depends on " +
		"//src/typed:typed for the module")

	// compile.d.mts takes a number; the JavaScript takes anything.
	test := it.Path("src/typed/compile.test.ts")
	restore := it.Read(test)
	it.Write(test, "import { compile } from \"./compile.mjs\";\n\nexport const s: string = compile(\"x\");\n")
	log, err := it.BazelLog("checked_in_declaration_is_in_force", "build",
		"//src/typed:typed_test")
	it.Write(test, restore)
	if err == nil {
		log.Dump()
		it.Fail("//src/typed:typed_test compiled compile(\"x\"); the " +
			"checked-in .d.mts is not the type in force")
	}
	if !log.Contains("TS2345") {
		log.Dump()
		it.Fail("//src/typed:typed_test failed for some other reason " +
			"than the argument")
	}
	it.Pass("compile(\"x\") is TS2345, so compile.d.mts types the import, not tsgo's inference from the .mjs")
}

// aliased/tsconfig.json maps #shared/* to ./shared/*; aliased/src extends it
// and is a package of its own, so its alias imports are deps on //aliased.
func aliasedImportsAreDeps(it *harness.IT) {
	for _, dir := range []string{"aliased", "aliased/src"} {
		it.RequireNotContains(it.Path(dir, "BUILD.bazel"), "path_alias",
			"//%s restates the tsconfig's alias through an attribute the rule does not have", dir)
	}
	it.Pass("no rule under //aliased carries an alias attribute")

	requireLabels(it, "srcs", "//aliased:aliased",
		[]string{"//aliased:shared/util.ts"})
	requireLabels(it, "srcs", "//aliased:aliased_test",
		[]string{"//aliased:shared/util.test.ts"})
	it.Pass("//aliased owns shared/, the files its own program lists and " +
		"aliased/src's does not")

	below := it.Path("aliased/src/BUILD.bazel")
	requireLabels(it, "deps", "//aliased/src:src", []string{"//aliased:aliased"})
	requireLabels(it, "deps", "//aliased/src:src_test",
		withRootManifest("//aliased/src:src", "//aliased:aliased"))
	it.Pass("both rules in //aliased/src depend on //aliased through the alias")

	tests := strings.Fields(it.BazelStdout("query", "tests(//aliased/...)"))
	if len(tests) != 2 {
		it.Fail("expected the two generated ts_tests under //aliased, got %v", tests)
	}
	it.Pass("%s ran under `bazel test //...` above, so both test programs resolved #shared/util", strings.Join(tests, " and "))

	// The measurement behind writing the dep at all: the alias maps to a file
	// only the dep edge stages.
	restore := it.Read(below)
	it.Replace(below, "    deps = [\"//aliased\"],\n", "")
	log, err := it.BazelLog("alias_without_the_dep", "build", "//aliased/src:src")
	it.Write(below, restore)
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
	requireLabels(it, "deps", "//src/env:env", []string{"@npm//:types_node"})
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
// runtime/, a package of its own; Icon.tsx imports nothing, so that is the dep.
func jsxRuntimeBecomesADep(it *harness.IT) {
	build := it.Path("jsx/BUILD.bazel")
	requireLabels(it, "deps", "//jsx:jsx", []string{"//jsx/runtime:runtime"})
	it.Pass("//jsx carries //jsx/runtime from jsxImportSource alone: nothing " +
		"in it imports")

	it.RequireFile(it.Bin("jsx/view/Icon.js"),
		"//jsx did not compile, so @acme/jsx/jsx-runtime never reached its program")
	it.Pass("//jsx type-checks a JSX tag against a runtime nothing imports")

	// The measurement behind writing the dep at all.
	restore := it.Read(build)
	it.Replace(build, "    deps = [\"//jsx/runtime\"],\n", "")
	log, err := it.BazelLog("jsx_runtime_without_the_dep", "build", "//jsx")
	it.Write(build, restore)
	if err == nil {
		log.Dump()
		it.Fail("//jsx compiled without the dep; the tsconfig alone reaches " +
			"the runtime and Gazelle need not write one")
	}
	if !log.Contains("TS2875") || !log.Contains("'@acme/jsx/jsx-runtime'") {
		log.Dump()
		it.Fail("//jsx failed for some other reason than the missing runtime")
	}
	it.Pass("without the dep the tag is TS2875 on '@acme/jsx/jsx-runtime': the implicit import resolves to nothing in the sandbox")
}

// worker/tsconfig.json states `types: ["./worker-configuration.d.ts"]` and
// handler.ts uses its declarations: the package holds both, restating nothing.
func declarationEntryResolvesThroughItsOwner(it *harness.IT) {
	owner := it.Path("worker/BUILD.bazel")
	requireLabels(it, "srcs", "//worker:worker",
		[]string{"//worker:src/handler.ts", "//worker:worker-configuration.d.ts"})
	requireLabels(it, "srcs", "//worker:worker_test",
		[]string{"//worker:src/handler.test.ts",
			"//worker:worker-configuration.d.ts"})
	for _, attr := range []string{"types =", "types_srcs", "tsconfig_types"} {
		it.RequireNotContains(owner, attr,
			"//worker restates the tsconfig's entry as %q", attr)
	}
	it.Pass("both rules in //worker hold the declaration the tsconfig names " +
		"and restate nothing")

	it.RequireFile(it.Bin("worker/src/handler.js"),
		"//worker did not compile, so the declarations never reached its program")
	it.Pass("//worker type-checks against declarations nothing there imports")

	theDeclarationStopsAtTheSubtree(it)
	parentEntryResolvesThroughTheOwner(it)
	packageEntryIsADep(it)
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
    deps = ["//worker"],
)

ts_compile(
    name = "leaked",
    srcs = ["leaked.ts"],
    deps = ["//worker"],
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
		requireLabels(it, "deps", testTarget(dir),
			withRootManifest("//worker:worker"))
		it.Pass("//%s depends on //worker and restates nothing", dir)
	}

	targets := strings.Fields(it.BazelStdout("query", "tests(//worker/test/...)"))
	if len(targets) != 2 {
		it.Fail("expected one generated ts_test in each of //worker/test and //worker/test/deep, got %v", targets)
	}
	it.Pass("%s ran under `bazel test //...` above, so each test program resolved its entry through //worker", strings.Join(targets, " and "))

	// The measurement behind writing the dep at all: the entry names a file only
	// the dep edge stages, and the rule refuses one nothing stages before tsgo.
	below := it.Path("worker/test/BUILD.bazel")
	restore := it.Read(below)
	it.Replace(below, "        \"//worker\",\n", "")
	log, err := it.BazelLog("types_entry_without_the_owner", "build",
		"//worker/test:test_test")
	it.Write(below, restore)
	if err == nil {
		log.Dump()
		it.Fail("//worker/test compiled without the dep; the entry alone " +
			"reaches the declaration and Gazelle need not write one")
	}
	if !log.Contains("worker/worker-configuration.d.ts, which no input of " +
		"this action sits at") {
		log.Dump()
		it.Fail("//worker/test failed for some other reason than the entry " +
			"naming a file nothing stages")
	}
	it.Pass("without the dep the entry names a file no input sits at: the " +
		"dep edge is what stages the declaration")
}

// src/app/tsconfig.json names vite/client and no file; the entry resolves through
// the chain, so what the BUILD file carries is the dep that puts vite on it.
func packageEntryIsADep(it *harness.IT) {
	build := it.Path("src/app/BUILD.bazel")
	it.RequireNotContains(build, "types =",
		"//src/app restates the tsconfig's package-only types list")
	if !slices.Contains(labels(it, "deps", "//src/app:app"), "@npm//:vite") {
		it.Fail("//src/app does not depend on the package its types entry names")
	}
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
	it.Pass("without the dep vite/client resolves to nothing on the chain: " +
		"the dep is what puts it in the program")
}
