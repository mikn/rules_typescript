package main

import (
	"fmt"
	"os"
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
		dirs := []string{"src/lib", "src/app", "src/icons", "worker", "worker/src"}

		// Written here rather than shipped in the workspace: a BUILD file under
		// a --deleted_packages entry is a package of the OUTER workspace, and
		// glob_workspace_files would stop collecting the fixture below it --
		// the same boundary this case is about.
		it.Write(it.Path("src/i18n/BUILD.bazel"), codegenGlobDirective)

		gazelleLog, err := it.BazelLog("gazelle_pass_1", "run", "//:gazelle")
		if err != nil {
			gazelleLog.Dump()
			it.Fail("bazel run //:gazelle exited non-zero: %v", err)
		}
		it.Pass("gazelle pass 1 complete")

		for _, dir := range dirs {
			it.RequireFile(it.Path(dir, "BUILD.bazel"), "Gazelle did not generate %s/BUILD.bazel", dir)
			it.Pass("%s/BUILD.bazel generated (pass 1)", dir)
		}

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
		codegenGlobLoads(it)
		assetDeclarationTypeApplies(it)
		anchoredExcludeHitsOnePath(it)
		jsSrcsAreCompiledAndDeclared(it)
		lintWithoutTheLinterIsRefused(it, gazelleLog)
	})
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

	targets := strings.Fields(it.BazelStdout("query", "kind(ts_test, //worker/...)"))
	if len(targets) != 1 {
		it.Fail("expected one generated ts_test under //worker, got %v -- the load below would be vacuous", targets)
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
