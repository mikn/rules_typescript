package main

import (
	"github.com/mikn/rules_typescript/tests/integration/harness"
)

func main() {
	harness.Run(harness.Config{
		Name:         "oxc_declarations",
		WorkspaceRel: "tests/integration/oxc_declarations",
	}, func(it *harness.IT) {
		it.MustBazel("run", "//:gazelle")
		it.Pass("bazel run //:gazelle")

		for _, dir := range []string{"src/lib", "src/bad"} {
			it.RequireFile(it.Path(dir, "BUILD.bazel"), "Gazelle did not generate %s/BUILD.bazel", dir)
		}

		for _, dir := range []string{"src/lib", "src/bad"} {
			build := it.Path(dir, "BUILD.bazel")
			if !it.Contains(build, `declarations = "oxc"`) {
				it.Dump(build)
				it.Fail("%s/BUILD.bazel is missing declarations = \"oxc\" (ts_declarations directive not respected)", dir)
			}
			it.Pass("%s/BUILD.bazel has declarations = \"oxc\"", dir)
		}

		// --output_groups=+_validation is explicit rather than in a .bazelrc:
		// under declarations = "oxc" the tsgo check lives in the validation
		// output group, and the next step deliberately builds WITHOUT it.
		if err := it.Bazel("build", "//src/lib:all", "--output_groups=+_validation"); err != nil {
			it.Fail("annotated target failed to build or type-check under declarations = \"oxc\"")
		}
		it.Pass("annotated target builds and type-checks under declarations = \"oxc\"")

		dts := it.Bin("src/lib/annotated.d.ts")
		it.RequireFile(dts, "oxc did not emit src/lib/annotated.d.ts")
		it.RequireContains(dts, "RegExp", "annotated.d.ts lost the RegExp type")
		it.Pass("oxc emitted annotated.d.ts with its declared types")

		bad, err := it.BazelLog("bad.log", "build", "//src/bad:all")
		if err == nil {
			bad.Dump()
			if badDTS := it.Bin("src/bad/inferred.d.ts"); it.Exists(badDTS) {
				it.Dump(badDTS)
			}
			it.Fail("un-annotated exports built under declarations = \"oxc\"; they must be rejected, never widened")
		}
		it.Pass("un-annotated exports were rejected under declarations = \"oxc\"")

		if !bad.Matches(`(?i)isolated declarations|TS901[0-9]|TS90[0-9][0-9]`) {
			bad.Dump()
			it.Fail("build failed, but not with an isolated-declarations diagnostic")
		}
		it.Pass("failure names the isolated-declarations problem")
	})
}
