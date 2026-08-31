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

	for _, want := range []string{"//shared:util.ts", "another package"} {
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
