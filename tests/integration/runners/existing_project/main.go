package main

import (
	"fmt"
	"strings"

	"github.com/mikn/rules_typescript/tests/integration/harness"
)

func main() {
	harness.Run(harness.Config{
		Name:         "existing_project",
		WorkspaceRel: "tests/integration/existing_project",
	}, func(it *harness.IT) {
		it.MustBazel("run", "//:gazelle")
		it.Pass("bazel run //:gazelle")

		build := it.Path("src/lib/BUILD.bazel")
		it.RequireFile(build, "Gazelle did not generate src/lib/BUILD.bazel")
		it.Pass("src/lib/BUILD.bazel generated")

		if strings.Contains(it.Read(build), "declarations") {
			it.Dump(build)
			it.Fail("Gazelle emitted a declarations attribute; the tsgo default needs none")
		}
		it.Pass("src/lib/BUILD.bazel has no declarations attribute (tsgo default)")

		it.MustBazel("build", "//src/lib:all")
		it.Pass("bazel build //src/lib:all")

		for _, rel := range []string{"src/lib/math.js", "src/lib/math.d.ts"} {
			it.RequireFile(it.Bin(rel), "expected output file not found: %s", rel)
			it.Pass("output file exists: %s", rel)
		}

		// This is the reason tsgo owns declaration emit: a syntactic emitter
		// cannot infer these and would widen them silently.
		dts := it.Bin("src/lib/math.d.ts")
		it.Dump(dts)
		for _, fn := range []string{"add", "multiply", "subtract", "divide"} {
			it.RequireMatches(dts, fmt.Sprintf(`declare function %s\(a: number, b: number\): number`, fn),
				"%s() lost its inferred 'number' return type in math.d.ts", fn)
			it.Pass("%s(): number inferred in math.d.ts", fn)
		}
		it.RequireNotMatches(dts, `:[[:blank:]]*(unknown|\{\})[[:blank:]]*;`,
			"math.d.ts widened an export to 'unknown' or '{}'")
		it.Pass("math.d.ts contains no widened exports")

		// Note the absence of --output_groups=+_validation: because the .d.ts are
		// real outputs of the tsgo action, a type error is a build failure.
		broken, err := it.BazelLog("broken.log", "build", "//src/broken:all")
		if err == nil {
			broken.Dump()
			it.Fail("//src/broken:all built successfully; a type error must fail the build")
		}
		it.Pass("//src/broken:all failed the build without --output_groups=+_validation")

		if !broken.Matches(`TS[0-9]{4}|not assignable`) {
			broken.Dump()
			it.Fail("build failed, but not with a type diagnostic")
		}
		it.Pass("failure names the type error")

		it.RequireNoFile(it.Bin("src/broken/type_error.d.ts"),
			"a failing target still produced src/broken/type_error.d.ts")
		it.Pass("no .d.ts was produced for the failing target")

		sharedSrc(it)
		srcsShapesStillBuild(it)
	})
}

// A file listed by a target in another package used to reach analysis, where it
// hit the one-rootDir check and was reported as "srcs hang off 2 different
// roots ... a mix of checked-in and generated sources" -- which names neither
// the file nor what is wrong with it. Written here rather than checked in so
// that Gazelle, which runs first, has no say in it.
func sharedSrc(it *harness.IT) {
	it.Write(it.Path("shared/BUILD.bazel"), "exports_files([\"util.ts\"])\n")
	it.Write(it.Path("shared/util.ts"), "export const util = 1;\n")
	it.Write(it.Path("consumer/main.ts"), "export const main = 1;\n")
	it.Write(it.Path("consumer/BUILD.bazel"), `load("@rules_typescript//ts:defs.bzl", "ts_compile")

ts_compile(
    name = "consumer",
    srcs = [
        "main.ts",
        "//shared:util.ts",
    ],
)
`)

	log, err := it.BazelLog("shared_src.log", "build", "//consumer:consumer")
	if err == nil {
		log.Dump()
		it.Fail("//consumer:consumer built; a src from //shared must be rejected")
	}
	it.Pass("//consumer:consumer failed")

	for _, want := range []string{"//shared:util.ts", "outside it"} {
		if !log.Contains(want) {
			log.Dump()
			it.Fail("the failure does not mention %q, so it is not the loading-phase check", want)
		}
	}
	if log.Contains("different roots") {
		log.Dump()
		it.Fail("the failure is still the analysis-time rootDir error")
	}
	it.Pass("the shared src is rejected while loading, naming the file")
}

// The four srcs shapes that are not the mix and build on origin/main: a
// descendant package's src, which hangs off this package like the target's own;
// a select; a canonical `@@//` label in this very package; and the top-level
// package, which IS the exec root a src from anywhere else hangs off.
func srcsShapesStillBuild(it *harness.IT) {
	it.Write(it.Path("holder/a.ts"), "export const a = 1;\n")
	it.Write(it.Path("holder/sub/x.ts"), "export const x = 1;\n")
	it.Write(it.Path("holder/sub/BUILD.bazel"), "exports_files([\"x.ts\"])\n")
	it.Write(it.Path("holder/BUILD.bazel"), `load("@rules_typescript//ts:defs.bzl", "ts_compile")

ts_compile(
    name = "holder",
    srcs = [
        "a.ts",
        "//holder/sub:x.ts",
    ],
)
`)

	it.Write(it.Path("chooses/a.ts"), "export const a = 1;\n")
	it.Write(it.Path("chooses/BUILD.bazel"), `load("@rules_typescript//ts:defs.bzl", "ts_compile")

ts_compile(
    name = "chooses",
    srcs = select({"//conditions:default": ["a.ts"]}),
)
`)

	it.Write(it.Path("canonical/a.ts"), "export const a = 1;\n")
	it.Write(it.Path("canonical/b.ts"), "export const b = 1;\n")
	it.Write(it.Path("canonical/BUILD.bazel"), `load("@rules_typescript//ts:defs.bzl", "ts_compile")

ts_compile(
    name = "canonical",
    srcs = [
        "a.ts",
        "@@//canonical:b.ts",
    ],
)
`)

	it.Write(it.Path("toplevel.ts"), "export const toplevel = 1;\n")
	it.Write(it.Path("BUILD.bazel"), it.Read(it.Path("BUILD.bazel"))+`
load("@rules_typescript//ts:defs.bzl", "ts_compile")

ts_compile(
    name = "toplevel",
    srcs = [
        "toplevel.ts",
        "//shared:util.ts",
    ],
)
`)

	it.MustBazel("build", "//holder:holder", "//chooses:chooses", "//canonical:canonical", "//:toplevel")
	it.Pass("bazel build //holder //chooses //canonical //:toplevel")

	for _, rel := range []string{
		"holder/a.d.ts",
		"holder/sub/x.d.ts",
		"chooses/a.js",
		"canonical/a.d.ts",
		"canonical/b.d.ts",
		"toplevel.d.ts",
		"shared/util.d.ts",
	} {
		it.RequireFile(it.Bin(rel), "a srcs shape that is not the mix lost its output: %s", rel)
	}
	it.Pass("a descendant src, a select, a canonical self-label and a top-level foreign src all still emit")
}
